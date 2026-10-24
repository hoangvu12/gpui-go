# Scene kernel service ABI (ticket08, reserved slot 3)

Status: implemented by `reference/native/src/scene.rs`, installed in
reserved slot 3 of the bootstrap `GpuiGoAbiTable` (see
`reference/native/src/lib.rs`) and advertised with capability bit 3
(`scene-kernel-v1`, and since ticket16 bit 7 `scene-draw-paths` for the
path/draw extension). The Go mirror lives in
`internal/native/scene.go`.

**Scene service version 5 (rev 15.1, browser-core ticket 77, native
revision 151):** the filter record grew from 188 to 780 bytes
(`MAX_SCENE_FILTERS` 4 to 8; `color_matrix` 20 to 160 words, one
20-word slot per colour function with the [0,1] clamp between — the
record section and `shaders/mod.rs` divergence 3 carry the full note).
No table slots changed; the scene table's `service_version` and
`size_of_filter_record` self-checks move with it.

**Scene records, rev 15 (browser-core ticket 39, native revision
15 — the service version stayed 4 through this growth):** the records
grew the browser-parity surface: `background` and
`border_color` (quads), `color` (shadows, paths) are full
`GpuiGoSceneBackground` records — a solid or a two-stop linear gradient
with CSS semantics (the gradient-line length `|W·sin θ| + |H·cos θ|`
reaches the corners, the stops mix premultiplied in the sRGB default;
both are recorded divergences from the pinned shader, compiled from the
vendored copy in `native/shaders/` by the crate's `build.rs`); drop
shadows clip out of the rounded `element_bounds` (CSS
css-backgrounds-3 §box-shadow, another recorded divergence); filter
records carry per-filter kind tags and the chain's 4x5 colour matrices
ONE PER COLOUR FUNCTION (CSS filter-effects' colour functions), applied
in chain order in the composite pass with the [0,1] clamp between
functions — rev 15.1 (browser-core ticket 77): Chrome clamps each
function's output before the next applies (every intermediate is
rasterized 8-bit), which one composed matrix cannot express (the
filters-chain fixture measured a 15.47 mean channel diff against
Chrome under the rev-15 composed payload);
sprite records carry a `pixelated` sampling flag (CSS
`image-rendering: pixelated`) that splits batches so one draws through
the point sampler; and the backdrop blur's input is cropped to the
element's pixel-covered bounds with mirrored margins (CSS
filter-effects-2, Chromium `SkTileMode::kMirror`) instead of blurring
the whole target.

**Scene service version 4 (gpui-perf ticket 05, native revision 13):**
the table gained slot 26, `insert_batch`
`(u64, *const u32 words, u32 word_count, *mut u32 out_applied) -> i32`,
which applies a packed op stream in order under one scene lock. Each op
is a two-word header `[kind, payload_words]` followed by the payload:
the exact record of the single-op entry (kinds 0 quad, 1 shadow,
2 underline, 3 backdrop filter, 4 filter boundary, 6 monochrome sprite,
7 subpixel sprite, 8 polychrome sprite), a surface record plus its
opacity bits (5), the four bounds words of a layer push (9), or nothing
for a layer pop (10) and an order-floor raise (11). Every op runs the
same validation and state transition as its single-op entry, so the
scene, its orders and its plan are identical; the call stops at the
first failing op and reports how many ops were applied. The Go wrapper
(`internal/native/scene_batch.go`) buffers every insert, layer push/pop
and floor raise and flushes before any read, finish, replay or path
insert. The table grew to 27 pointers, 17 scalars, size 288.

**Scene service version 3 (ticket16):** the `Path` primitive class is
supported: `scene_insert_path` (a path record plus its bulk vertex
array), the `path_dump` bulk dump, the `path_script` tessellation entry
(the pinned `PathBuilder` public API driven by a command array), the
`paths.sort_by_key(order)` finish step, the pinned `BatchIterator` Path
arm (batch merging with `rasterization_vertex_count` and
`sprite_count`), the `Paths` requirements (two instance batches,
`uses_path_target`), `path_count` in the meta record and `PaintOp::Path`
replay. The table layout changed (26 pointers, 17 scalars, size 280);
the record-size self-checks catch pre-v3 mirrors.

**Scene service version 2 (ticket10):** the three sprite primitive
classes (`MonochromeSprite`, `SubpixelSprite`, `PolychromeSprite` —
atlas tiles drawn as textured quads) are supported: three new
insert entries, three new dump kinds, the `(order, tile_id)` finish
tie-break, per-texture batch splitting and the sprite counts in the
meta record. (The v2 table layout: 23 pointers, 14 scalars, size 240.)

The semantics are the pinned GPUI-CE scene kernel at commit
`254b5dbd47cbb5acbcc5bbdcbb322a339276c88a`
(`crates/gpui/src/scene.rs`, `crates/gpui/src/scene/plan.rs`,
`crates/gpui/src/bounds_tree.rs`): the service is a port of that kernel,
not a reimplementation of its behavior from scratch. The independent
oracle for this service is the pinned crate itself, exercised through
its public `Scene` API by the reference harness
(`reference/harness/src/fixtures/scene_painting.rs`, fixture family
`scene-painting-v1`).

## What the service owns

- Scene creation/disposal with generation handles.
- Primitive insertion with bounds-tree draw-order computation: one
  greater than the maximum order of any intersecting bounds, raised to
  the order floor; a primitive inside a layer carries the LAYER's order
  (the pinned `layer_stack.last()` fallback).
- Layer push/pop (`push_layer`/`pop_layer`), deferred-draw order floor
  (`raise_order_floor`), content-filter boundary insertion (always
  inserts, order above all prior content; the END marker raises the
  floor above itself).
- Empty-clipped primitive dropping (ordinary primitives disappear;
  filter boundaries survive with their primitive bounds).
- `finish`: stable sort of each primitive array by order (boundaries
  additionally by `!is_start`), surface/opacity pairing through the
  sort, and render-plan compilation.
- The render plan: batch merging (runs of one primitive kind split by
  corner smoothing), filter-boundary emission one at a time, filter
  pairing and isolation-target assignment (`MAX_FILTER_GROUP_DEPTH` =
  2; deeper groups inline), and requirements collection.
- Replay of a range of a previous scene's paint operations into a
  target scene (orders recomputed against the target; surfaces keep
  their paired opacities).
- Bulk dumps of the finished scene's primitive arrays, plan commands,
  requirements and totals into caller buffers.

Supported primitive classes: `Quad`, `Shadow`, `Underline`,
`BackdropFilter`, `FilterBoundary`, `Surface` (with paired opacity),
the sprite classes `MonochromeSprite`, `SubpixelSprite` and
`PolychromeSprite` (the pinned finish sort
`sort_by_key(|sprite| (sprite.order, sprite.tile.tile_id))`, the
per-texture batch splitting of the pinned `BatchIterator`, sprite
batches counting as instance batches in the requirements), and since
ticket16 `Path` (the tessellated triangle list: the pinned finish sort
`paths.sort_by_key(|path| path.order)`, the pinned Path batch arm with
`rasterization_vertex_count` = the summed vertex count and
`sprite_count` = the path count when the batch's paths share the
first's order (disjoint bounds) else 1 (one spanning sprite), and the
pinned `include_batch` Paths requirements: `path_rasterization_vertex_count`,
`path_sprite_count`, `uses_path_target` and `instance_batch_count += 2`
when the vertex count is nonzero). The primitive-kind tie-break order
(the pinned `PrimitiveKind` discriminants) is preserved.

## Records

All records are `#[repr(C)]`, alignment 4, every f32 field carried as
IEEE-754 bits (u32). The Go mirrors assert `unsafe.Sizeof` against the
service table's self-check fields before any call.

| Record | Size | Layout (offsets) |
|---|---|---|
| `GpuiGoSceneBounds` | 16 | x @0, y @4, w @8, h @12 (f32 bits) |
| `GpuiGoSceneColor` | 20 | tag @0 (0 = solid), h @4, s @8, l @12, a @16 (f32 bits; h is degrees/360 in `[0,1]`) |
| `GpuiGoSceneBackground` | 72 | tag @0 (0 = solid, 1 = linear gradient), color_space @4 (0 = sRGB, 1 = Oklab), h @8, s @12, l @16, a @20, angle @24, stop0_h @28, stop0_s @32, stop0_l @36, stop0_a @40, stop0_percentage @44, stop1_h @48, stop1_s @52, stop1_l @56, stop1_a @60, stop1_percentage @64, reserved @68 (all f32 bits; the stops' percentages are the CSS positions along the gradient line and must differ) |
| `GpuiGoSceneQuadRecord` | 236 | order @0, bounds @4, content_mask @20, background @36, border_color @108, corner_radii[4] @180, border_widths[4] @196, border_style @212, border_dashed_length @216, border_dashed_gap @220, corner_smoothing @224, padding @228, record_size @232 |
| `GpuiGoSceneShadowRecord` | 172 | order @0, blur_radius @4, bounds @8, corner_radii[4] @24, content_mask @40, color @56, element_bounds @128, element_corner_radii[4] @144, inset @160, corner_smoothing @164, record_size @168 |
| `GpuiGoSceneUnderlineRecord` | 72 | order @0, padding @4, bounds @8, content_mask @24, color @40, thickness @60, wavy @64, record_size @68 |
| `GpuiGoSceneFilterRecord` | 780 | order @0, bounds @4, content_mask @20, corner_radii[4] @36, corner_smoothing @52, blur_radii[8] @56, filter_count @88, opacity @92, is_start @96, filter_tags[8] @100 (per-filter: 0 = blur, 1 = colour matrix; unused entries zero), color_matrix[160] @132 (rev 15.1: one row-major 4x5 matrix slot PER matrix filter in chain order — slot i is words 20i..20i+20, f32 bits; slots at and beyond color_matrix_count zero), color_matrix_count @772 (the tag-1 entry count), record_size @776 |
| `GpuiGoSceneSurfaceRecord` | 56 | order @0, bounds @4, content_mask @20, source_tag @36, source_reserved[3] @40, record_size @56 |
| `GpuiGoSceneTileRecord` | 32 | texture_index @0, texture_kind @4, tile_id @8, padding @12, bounds x/y/w/h @16..32 (the pinned `AtlasTile` sub-record of the sprite records) |
| `GpuiGoSceneSpriteRecord` | 124 | order @0, padding @4, bounds @8, content_mask @24, color @40, tile @60, transformation[6] @92 (rotation-scale 2x2 then translation, f32 bits), pixelated @116 (0 = bilinear, 1 = point sampling), record_size @120 (the pinned `MonochromeSprite`/`SubpixelSprite` field set) |
| `GpuiGoScenePolychromeSpriteRecord` | 104 | order @0, grayscale @4, opacity @8, corner_smoothing @12, bounds @16, content_mask @32, corner_radii[4] @48, tile @64, pixelated @96, record_size @100 |
| `GpuiGoSceneCommandRecord` | 56 | command_kind @0, primitive_kind @4, range_start @8, range_end @12, smoothed @16, pixelated @20, texture_index @24, rasterization_vertex_count @28, sprite_count @32, boundary_index @36, closing_boundary_index @40, filter_target @44, target_index @48, record_size @52 |
| `GpuiGoSceneRequirementsRecord` | 44 | command_count @0, instance_batch_count @4, path_rasterization_vertex_count @8, path_sprite_count @12, surface_count @16, backdrop_filter_count @20, isolated_filter_count @24, isolated_target_count @28, uses_path_target @32, uses_offscreen_target @36, record_size @40 |
| `GpuiGoSceneMetaRecord` | 56 | op_count @0, layer_push_count @4, quad_count @8, shadow_count @12, underline_count @16, backdrop_count @20, boundary_count @24, surface_count @28, monochrome_sprite_count @32, subpixel_sprite_count @36, polychrome_sprite_count @40, path_count @44, is_finished @48, record_size @52 |
| `GpuiGoScenePathRecord` | 116 | order @0, bounds @4, content_mask @20, color @36, vertex_count @108, record_size @112 |
| `GpuiGoScenePathVertexRecord` | 16 | xy_x @0, xy_y @4, st_u @8, st_v @12 (f32 bits) |
| `GpuiGoPathCommandRecord` | 56 | kind @0, data[12] @4, record_size @52 |

`order` fields mirror the pinned `DrawOrder` (u32): **must be 0 on
insert** (the kernel assigns it) and carry the assigned order in dumps.
Replay re-inserts stored records whose order field is non-zero; that is
an internal path and the kernel overwrites the order (validation of the
zero-order rule happens only at the external ABI boundary).

Enums: `border_style` 0 solid / 1 dashed; `inset`/`wavy`/`is_start`/the
`smoothed` command field 0/1; `command_kind` 0 batch / 1 begin-filter /
2 end-filter; `primitive_kind` 0 shadows, 1 quads, 2 paths, 3
underlines, 4 monochrome sprites, 5 subpixel sprites, 6 polychrome
sprites, 7 surfaces, 8 backdrop filters, 9 filter boundary;
`filter_target` 0 inline / 1 isolated (with the target pool index in
`target_index`).

## Table

`GpuiGoSceneTable`: 26 function pointers then 17 self-check scalars;
size 280, alignment 8. Function entries (all `extern "system"`, all
returning an i32 status):

| Slot | Entry | Signature |
|---|---|---|
| 0 | `create` | `(*mut u64 handle) -> i32` |
| 1 | `clear` | `(u64 handle) -> i32` |
| 2 | `dispose` | `(u64 handle) -> i32` |
| 3 | `push_layer` | `(u64, *const GpuiGoSceneBounds) -> i32` |
| 4 | `pop_layer` | `(u64) -> i32` |
| 5 | `raise_order_floor` | `(u64) -> i32` |
| 6 | `insert_quad` | `(u64, *const GpuiGoSceneQuadRecord) -> i32` |
| 7 | `insert_shadow` | `(u64, *const GpuiGoSceneShadowRecord) -> i32` |
| 8 | `insert_underline` | `(u64, *const GpuiGoSceneUnderlineRecord) -> i32` |
| 9 | `insert_backdrop_filter` | `(u64, *const GpuiGoSceneFilterRecord) -> i32` |
| 10 | `insert_filter_boundary` | `(u64, *const GpuiGoSceneFilterRecord) -> i32` |
| 11 | `insert_surface` | `(u64, *const GpuiGoSceneSurfaceRecord, u32 opacity_bits) -> i32` |
| 12 | `insert_monochrome_sprite` | `(u64, *const GpuiGoSceneSpriteRecord) -> i32` |
| 13 | `insert_subpixel_sprite` | `(u64, *const GpuiGoSceneSpriteRecord) -> i32` |
| 14 | `insert_polychrome_sprite` | `(u64, *const GpuiGoScenePolychromeSpriteRecord) -> i32` |
| 15 | `replay` | `(u64 target, u32 start, u32 end, u64 prev) -> i32` |
| 16 | `finish` | `(u64) -> i32` |
| 17 | `len` | `(u64, *mut u32) -> i32` |
| 18 | `meta` | `(u64, *mut GpuiGoSceneMetaRecord) -> i32` |
| 19 | `dump` | `(u64, u32 kind, *mut u8 records, *mut u32 opacity_bits, u32 capacity, *mut u32 out_count) -> i32` |
| 20 | `plan_dump` | `(u64, *mut GpuiGoSceneCommandRecord, u32 capacity, *mut u32 out_count) -> i32` |
| 21 | `requirements` | `(u64, *mut GpuiGoSceneRequirementsRecord) -> i32` |
| 22 | `panic_probe` | `() -> i32` (test-only; panics inside catch_unwind and reports `ERR_PANIC`) |
| 23 | `insert_path` | `(u64, *const GpuiGoScenePathRecord, *const GpuiGoScenePathVertexRecord, u32 vertex_count) -> i32` (ticket16) |
| 24 | `path_dump` | `(u64, *mut GpuiGoScenePathRecord, *mut GpuiGoScenePathVertexRecord, u32 record_capacity, u32 vertex_capacity, *mut u32 out_record_count, *mut u32 out_vertex_count) -> i32` (ticket16) |
| 25 | `path_script` | `(*const GpuiGoPathCommandRecord, u32 command_count, *const u32 points, u32 word_count, *mut GpuiGoScenePathVertexRecord, u32 vertex_capacity, *mut u32 out_vertex_count, *mut GpuiGoSceneBounds out_bounds) -> i32` (ticket16) |
| 26 | `insert_batch` | `(u64, *const u32 words, u32 word_count, *mut u32 out_applied) -> i32` (version 4, gpui-perf ticket 05; see above) |

Self-check scalars: `service_version` (4), `size_of_table` (288),
`align_of_table` (8), `size_of_quad_record` (132),
`size_of_shadow_record` (120), `size_of_underline_record` (72),
`size_of_filter_record` (780), `size_of_surface_record` (56),
`size_of_monochrome_sprite_record` (120),
`size_of_polychrome_sprite_record` (100),
`size_of_command_record` (52), `size_of_requirements_record` (44),
`size_of_meta_record` (56), `size_of_path_record` (64),
`size_of_path_vertex_record` (16), `size_of_path_command_record` (56),
`max_filters` (8).

## Handles

Scene handle: `(slot index << 20) | slot generation` (the same encoding
as the layout engine handles). A handle is stale after `dispose` or
slot reuse; a malformed handle never existed. Generation overflow
(2^20-1 reuses of one slot) returns an error instead of aliasing.

## Status codes

0 ok, -1 stale handle, -2 bad handle, -3 null argument, -4 bad value
(record-size mismatch, non-zero order on insert, unsupported tag or
enumerant, filter count over the bound, unknown dump kind, arithmetic
overflow, same-scene re-entry, self-replay, malformed replay range), -5
not finished (a plan/requirements/dump read before `scene_finish`), -6
capacity (dump capacity too small; `out_count` carries the required
count), -7 scene failed (after a contained panic; clear or dispose
required), -8 limit (an insert exceeded a scene capacity bound), 101
panic contained.

## Capacity bounds

- 262,144 paint operations per scene (the replay address space),
- 65,536 primitives per class per scene,
- 8 filters per chain (rev 15.1 grew the pinned `SmallVec
  <[ScaledFilter; 4]>` capacity 4 to 8 so a chain carries the
  per-function colour matrices beside the composed blur; a longer
  chain composes its excess into the last matrix slot — the
  documented approximation),
- 262,144 plan commands per scene.

## Validation order

Null pointers first, then record validation (record size, zero order,
tags, booleans, filter count, zeroed reserved fields), then handle
resolution, then the operation. A rejected insert publishes nothing.

## Contract notes

- Every export contains panics with `catch_unwind`; a contained panic
  marks the scene failed (subsequent calls return the failed status
  until `clear`/`dispose`, mirroring the layout engine's failure
  semantics).
- Dumps require `finish` (the pinned `render_plan` debug-asserts this;
  the service turns it into an explicit error). A dump's capacity is in
  records; a too-small capacity reports the required count and copies
  nothing. The records buffer must be 4-byte aligned (Go slices of the
  record mirrors are).
- `scene_dump` kinds: 0 quads, 1 shadows, 2 underlines, 3 backdrop
  filters, 4 filter boundaries, 5 surfaces (surfaces additionally fill
  the parallel `opacity_bits` array — the paired opacities), 6
  monochrome sprites, 7 subpixel sprites, 8 polychrome sprites.
- Sprites finish-sort by `(order, tile_id)` (the pinned
  `sort_by_key(|sprite| (sprite.order, sprite.tile.tile_id))`), the
  plan batches split on texture changes exactly as the pinned
  BatchIterator sprite arms do (polychrome batches additionally split
  on corner smoothing), and sprite batches count into
  `instance_batch_count` (the pinned `include_batch`).
- Sprite records validate the tile's texture-kind/pool pairing at
  insert (monochrome sprites carry monochrome-pool tiles, subpixel
  sprites subpixel-pool tiles, polychrome sprites polychrome-pool
  tiles): the renderer contract's "never reinterpret one format as
  another".
- `replay` copies the source's operations out under the source's lock,
  then re-inserts them into the target; the target's orders are
  recomputed against the target's bounds tree and layer state. A scene
  cannot replay itself (the pinned signature borrows the source
  immutably and the target mutably). The source stays pinned by the
  caller for as long as replay ranges reference it.
- No Go pointers are retained across calls; all records are copied at
  insert and at dump.
- The service performs no color-space conversion: `GpuiGoSceneColor`
  carries the stored HSL-with-alpha record exactly as the pinned
  `Background` solid form does. Color conversion (rgba -> hsla) is a
  Go-side responsibility following the pinned `rgb_to_hsla`.
- `path_script` (ticket16) replays a validated command array onto the
  pinned `gpui::PathBuilder` public API — the same lyon code the
  reference oracle drives — and returns the tessellated vertices
  (logical pixels) plus the bounds of the tessellation (the union of
  the triangles, `(0,0,0,0)` for an empty tessellation, mirroring the
  pinned `build_path` fallback). Vertex st coordinates of tessellated
  paths are the pinned constant `(0.0, 1.0)`. The style and dash-array
  commands are setters applied before `build()`, exactly like the
  pinned `with_style`/`dash_array` builder chain; a lyon build failure
  maps to `ERR_BAD_VALUE`. Command validation covers record sizes,
  known kind tags, finite floats, enum tag ranges, boolean tags and
  word-pool indices; reserved data words must be zero.
- `path_dump` (ticket16) bulk-dumps the finished scene's path records
  (with the assigned orders and per-path vertex counts) and the
  concatenated vertex array; both capacities are checked with the
  required counts reported in the out parameters.
- Path vertex arrays are bounded by `MAX_SCENE_PATH_VERTICES`
  (1,048,576) per inserted path.
- The renderer-side consumption (ticket16) resolves a finished scene
  through `scene_render_snapshot` (crate-internal): the records are
  copied into the pinned `#[repr(C)]` GPUI-CE primitive layouts the
  generated shaders were compiled against. The ABI color round trip
  goes through the public `Background::from(Hsla)`; the pinned
  per-path `Path::id` is not carried (assigned at insertion, read
  nowhere).
