# Scene kernel service ABI (ticket08, reserved slot 3)

Status: implemented by `reference/native/src/scene.rs`, installed in
reserved slot 3 of the bootstrap `GpuiGoAbiTable` (see
`reference/native/src/lib.rs`) and advertised with capability bit 3
(`scene-kernel-v1`, and since ticket16 bit 7 `scene-draw-paths` for the
path/draw extension). The Go mirror lives in
`internal/native/scene.go`.

**Scene service version 8 (rev 16, browser-core ticket 52, native
revision 151 - ticket 54 bumps the revision):** the mask, blend,
gradient and drop-shadow half of the R16 effect group lands. The table
appends four entries after `update_property`: `scene_insert_gradient`
(slot 30: one gradient table entry + its bulk stop array; the service
bakes a 1024x1 premultiplied-sRGB ramp row per gradient and assigns
`stop_row` = its own table index), `scene_gradient_dump` (slot 31: the
finished scene's gradient records + the concatenated stops),
`scene_insert_coverage` (slot 32: the clip-path coverage entries) and
`scene_insert_filter_op_chain` (slot 33: the drop-shadow op chains) -
34 pointers, 376 bytes, five new self-check scalars
(size_of_gradient_record, size_of_gradient_stop, max_gradient_stops,
size_of_coverage_record, size_of_filter_op_record). The
`insert_batch` stream gains op kinds 14 (gradient: the 14-word record
+ the 2-word stop rows), 15 (coverage: 6 words) and 16 (op chain: a
count word + the 10-word op rows). Record changes, all append-only:
- the effect record's mask kinds 1-3 are WIRED (kind 0 with a
  non-zero reference/mode still rejects; the REFERENCES resolve at
  RENDER time - an unresolvable mask hides the element, the design
  3.5's transparent-black rule - so replayed ranges stay robust); a
  blend group forces `uses_offscreen_target` (the composite samples a
  backdrop copy);
- the background's tag 4 is the gradient-TABLE arm (the table index
  rides the ANGLE field as f32 bits; allowed on quads and shadows,
  rejected on paths); the single-primitive opacity fold SKIPS
  quads/shadows carrying it (the fold multiplies the record's stored
  alpha payload, which the table arm carries none of - such a group
  isolates);
- the filter record appends `op_chain_index`/`op_chain_count` after
  `record_size` (820 -> 828 bytes; 0/0 = the R15 legacy chain,
  bit-exact);
- the meta record appends `gradient_count`/`coverage_count`/
  `filter_chain_count` after `record_size` (60 -> 72 bytes).

**Scene service version 7 (rev 16, browser-core ticket 51, native
revision 151 - ticket 54 bumps the revision):** the transform and
property-node half of the R16 effect group landed. The ONLY change was
the table entry `scene_update_property` (slot 29, after
`effect_dump`); no records or scalars changed, so the table grew 312
-> 320 bytes. A `has_transform` effect group now composites under its
2x3 matrix (the subtree renders into the group's LOCAL frame - the
records' own coordinates - and the composite maps it through the
row-major `[m00, m01, m10, m11, tx, ty]`; the transform-origin and the
group's parent-frame position are resolved by the CALLER into the
matrix, CSS-style; translation-only matrices composite through the
axis-aligned path at the translated rect). `scene_update_property`
mutates a FINISHED scene's effect records in place:

```
i32 scene_update_property(u64 scene, u32 node_id, u32 kind,
    const u32* values, u32 value_count,
    GpuiGoSceneBounds* out_old, GpuiGoSceneBounds* out_new)
```

- `kind` 0 = matrix (six f32 words, the record's `transform` layout;
  the node's records must carry `has_transform`), 1 = opacity (one
  f32 word in [0,1]; the records must carry `has_opacity`);
- every effect record with `node_id` is mutated - no re-sort, no
  re-plan (the plan's commands and orders never move; the render walk
  consults the current records, so the same scene handle re-presents
  with the updated value);
- `out_old`/`out_new` receive the group's device bounds mapped
  through the old and the new matrix (the record bounds mapped by the
  corners' AABB; the raw record bounds when the node has no
  transform) - the damage union the caller re-presents (the Go frame
  loop of ticket 60). Both pointers may be null (no report);
- errors: `ERR_NOT_FINISHED` before `scene_finish`, `ERR_NULL_ARG`
  for null values, `ERR_BAD_VALUE` for a zero node id, an unknown
  kind, a wrong value count, a stale id (no record carries it), a
  missing required flag, or a value the insert validation would
  reject (a non-finite or singular 2x2, an opacity outside [0,1]).

**Scene service version 6 (rev 16, browser-core ticket 50, native
revision 151):** the R16 group machinery lands. The design doc's
"scene service v5" became this version 6 because ticket 77 had
already taken 5. Everything below is append-only; the pre-R16 record
meanings are unchanged and a zero-radii, zero-fade mask behaves
exactly as the old 16-byte bounds mask (group-free scenes render
pixel-identically):

- the **effect group** primitive class 10: `GpuiGoSceneEffectRecord`
  (116 bytes) with `insert_effect` / `effect_dump` (table slots 27/28)
  and the `insert_batch` op kinds 12/13 (effect start / end, the
  record payload with `flags.IS_START` forced by the kind); the plan
  compiles matched pairs into command kinds **3 (begin effect group)**
  and **4 (end effect group)**, sharing ONE group stack with the R15
  filter boundaries;
- every record's **content mask grows from 16 to 52 bytes**
  (`GpuiGoSceneContentMask`: bounds + per-edge `fade_out` (LEFT, TOP,
  RIGHT, BOTTOM order) + corner radii + smoothing) - the pin's
  shader-side mask, finally carried; the innermost rounded clip
  applies inline in the vendored quad/shadow pipelines and the
  per-edge fades ride every masked pipeline (quads, shadows, filter
  records, surfaces, both sprite records, paths - each record grows by
  the 40-byte mask: quad 236->280, shadow 172->216, underline
  72->112, filter 780->820, surface 56->96, sprite 124->164,
  polychrome sprite 104->144, path 116->156; the quad and shadow
  records also take the PINNED field order plus trailing layout pads
  because they ARE the vendored pipelines' instance layouts - the HLSL
  structured-buffer layout aligns the vec2f-bearing fields to 8
  bytes);
- `MAX_FILTER_GROUP_DEPTH` (2) is REMOVED: every matched group of
  either kind gets an isolated pool target slot (the target_index in
  the begin command; scenes nested at most two deep produce EXACTLY
  the R15 plan); the new sanity bound `MAX_SCENE_GROUPS` = 1024 counts
  group START markers of both kinds and returns `ERR_LIMIT` when the
  start that would exceed it is inserted; the renderer's per-surface
  group-target pool (64 targets / 256 MB) is the real budget and
  demotes over-budget groups to inline, observably;
- the **single-primitive opacity fold**: a pure-opacity effect group
  whose span holds exactly one primitive and no nested group renders
  INLINE (the begin/end commands carry `filter_target` 0) and the
  group's opacity multiplies that primitive's colour at snapshot time
  (the stored records stay as inserted; dumps read them unchanged);
- `GpuiGoSceneRequirementsRecord` appends `isolated_effect_count` (44
  -> 48) and `GpuiGoSceneMetaRecord` appends `effect_count` (56 -> 60).

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
| `GpuiGoSceneContentMask` | 56 | bounds @0 (16), fade_out[4] @16 (left, top, right, bottom, f32 bits; a zero distance leaves that edge opaque), corner_radii[4] @32 (TL, TR, BR, BL, f32 bits), corner_smoothing @48 (f32 bits), mask_padding @52 (must be zero - the mask's HLSL structured-buffer size rounds up to its 8-byte alignment; the record IS the vendored quad/shadow pipelines' instance layout) |
| `GpuiGoSceneEffectRecord` | 116 | order @0, flags @4 (bit 0 `is_start`, 1 `has_transform`, 2 `has_opacity`, 3 `has_blend`, 4 `has_mask`, 5 `property_node`, 6 `rounded_clip_only`, 7 `clip_to_bounds`), bounds @8, content_mask @24 (a plain 16-byte bounds record - the group's rrect comes from corner_radii/clip_to_bounds), transform[6] @40 (row-major m00, m01, m10, m11, tx, ty, f32 bits - the LOCAL-to-parent map: `device = m00*p.x + m01*p.y + tx`, `m10*p.x + m11*p.y + ty`, requiring an invertible 2x2 when `has_transform` is set), opacity @64 ([0,1] f32 bits - the composite's multiplier, ALWAYS applied; set 1 for groups that do not fade), blend_mode @68 (0..16, see the design's A.1; rev 16 ticket 52 wires the W3C blend formula in the composite), mask_kind @72 (0 none; 1 gradient table, 2 atlas texture, 3 coverage - rev 16 ticket 52), mask_reference @76 (the gradient table index / the atlas texture index / the coverage entry index; resolved at RENDER, an unresolvable mask hides the group), mask_mode @80 (0 alpha, 1 luminance), node_id @84 (0 = none), corner_radii[4] @88, corner_smoothing @104, reserved @108 (0), record_size @112 |
| `GpuiGoSceneGradientRecord` | 56 | kind @0 (0 linear, 1 radial, 2 conic), tile @4 (0 pad, 1 repeat, 2 reflect), color_space @8 (0 sRGB only this revision), geometry[8] @12 (f32 bits: linear `[x0, y0, x1, y1]`; radial `[cx, cy, r0, r1, aspect]` with the radii in ry units and aspect = rx/ry; conic `[cx, cy, start_angle, rotation]` with the angles in RADIANS, clockwise from up, rotation = 2*pi for a full turn), stop_count @44 (1..=64), stop_row @48 (0 on insert; the service assigns the gradient's own table index, 1:1), record_size @52 |
| `GpuiGoSceneGradientStop` | 8 | position @0 (f32 bits, [0,1], non-decreasing across the array), rgba @4 (straight sRGBA, one byte per lane: r / g<<8 / b<<16 / a<<24) |
| `GpuiGoSceneCoverageRecord` | 24 | path_index @0 (an index into the scene's path records; resolved at render), bounds @4 (4 words), record_size @20 |
| `GpuiGoSceneFilterOpRecord` | 40 | kind @0 (2 = drop-shadow, the only insertable kind), offset[2] @4 (dx, dy f32 bits), blur_radius @12 (the CSS blur radius, f32 bits; the renderer derives sigma = radius/2), color @16 (a solid `GpuiGoSceneColor`), record_size @36 |
| `GpuiGoSceneColor` | 20 | tag @0 (0 = solid), h @4, s @8, l @12, a @16 (f32 bits; h is degrees/360 in `[0,1]`) |
| `GpuiGoSceneBackground` | 72 | tag @0 (0 = solid, 1 = linear gradient), color_space @4 (0 = sRGB, 1 = Oklab), h @8, s @12, l @16, a @20, angle @24, stop0_h @28, stop0_s @32, stop0_l @36, stop0_a @40, stop0_percentage @44, stop1_h @48, stop1_s @52, stop1_l @56, stop1_a @60, stop1_percentage @64, reserved @68 (all f32 bits; the stops' percentages are the CSS positions along the gradient line and must differ) |
| `GpuiGoSceneQuadRecord` | 280 | order @0, border_style @4, border_dashed_length @8, border_dashed_gap @12, bounds @16, content_mask @32 (56 bytes), background @88, border_color @160, corner_radii[4] @232, border_widths[4] @248, corner_smoothing @264, padding @268, layout_padding @272 (zero), record_size @276. The PINNED field order (the vec2f-bearing fields land on 8-byte-aligned offsets - exactly why the pin's order is what it is): the record IS the vendored quad pipeline's instance layout, uploaded directly |
| `GpuiGoSceneShadowRecord` | 216 | order @0, blur_radius @4, bounds @8, corner_radii[4] @24, content_mask @40 (56 bytes), color @96, element_bounds @168, element_corner_radii[4] @184, inset @200, corner_smoothing @204, layout_padding @208 (zero), record_size @212. The record IS the vendored shadow pipeline's instance layout, uploaded directly |
| `GpuiGoSceneUnderlineRecord` | 112 | order @0, padding @4, bounds @8, content_mask @24 (56 bytes), color @80, thickness @100, wavy @104, record_size @108 |
| `GpuiGoSceneFilterRecord` | 828 | order @0, bounds @4, content_mask @20 (56 bytes), corner_radii[4] @76, corner_smoothing @92, blur_radii[8] @96, filter_count @128, opacity @132, is_start @136, filter_tags[8] @140 (per-filter: 0 = blur, 1 = colour matrix; unused entries zero), color_matrix[160] @132 (rev 15.1: one row-major 4x5 matrix slot PER matrix filter in chain order — slot i is words 20i..20i+20, f32 bits; slots at and beyond color_matrix_count zero), color_matrix_count @808 (the tag-1 entry count), record_size @812, op_chain_index @816 (rev 16 ticket 52: the drop-shadow chain reference, appended after record_size), op_chain_count @820 (0/0 = the R15 legacy chain, bit-exact) |
| `GpuiGoSceneSurfaceRecord` | 96 | order @0, bounds @4, content_mask @20 (56 bytes), source_tag @76, source_reserved[3] @80, record_size @92 |
| `GpuiGoSceneTileRecord` | 32 | texture_index @0, texture_kind @4, tile_id @8, padding @12, bounds x/y/w/h @16..32 (the pinned `AtlasTile` sub-record of the sprite records) |
| `GpuiGoSceneSpriteRecord` | 164 | order @0, padding @4, bounds @8, content_mask @24 (56 bytes), color @80, tile @100, transformation[6] @132 (rotation-scale 2x2 then translation, f32 bits), pixelated @156 (0 = bilinear, 1 = point sampling), record_size @160 (the pinned `MonochromeSprite`/`SubpixelSprite` field set) |
| `GpuiGoScenePolychromeSpriteRecord` | 144 | order @0, grayscale @4, opacity @8, corner_smoothing @12, bounds @16, content_mask @32 (56 bytes), corner_radii[4] @88, tile @104, pixelated @136, record_size @140 |
| `GpuiGoSceneCommandRecord` | 56 | command_kind @0, primitive_kind @4, range_start @8, range_end @12, smoothed @16, pixelated @20, texture_index @24, rasterization_vertex_count @28, sprite_count @32, boundary_index @36, closing_boundary_index @40, filter_target @44, target_index @48, record_size @52 |
| `GpuiGoSceneRequirementsRecord` | 48 | command_count @0, instance_batch_count @4, path_rasterization_vertex_count @8, path_sprite_count @12, surface_count @16, backdrop_filter_count @20, isolated_filter_count @24, isolated_target_count @28 (the pool high-water mark over BOTH group kinds - a conservative upper bound when the fold patches a begin), uses_path_target @32, uses_offscreen_target @36 (rev 16 ticket 52: set for filter groups AND blend groups - the blend composite samples a backdrop copy of the parent), record_size @40, isolated_effect_count @44 (rev 16) |
| `GpuiGoSceneMetaRecord` | 72 | op_count @0, layer_push_count @4, quad_count @8, shadow_count @12, underline_count @16, backdrop_count @20, boundary_count @24, surface_count @28, monochrome_sprite_count @32, subpixel_sprite_count @36, polychrome_sprite_count @40, path_count @44, effect_count @48 (rev 16), is_finished @52, record_size @56, gradient_count @60 (rev 16 ticket 52), coverage_count @64, filter_chain_count @68 |
| `GpuiGoScenePathRecord` | 156 | order @0, bounds @4, content_mask @20 (56 bytes), color @76, vertex_count @148, record_size @152 |
| `GpuiGoScenePathVertexRecord` | 16 | xy_x @0, xy_y @4, st_u @8, st_v @12 (f32 bits) |
| `GpuiGoPathCommandRecord` | 56 | kind @0, data[12] @4, record_size @52 |

`order` fields mirror the pinned `DrawOrder` (u32): **must be 0 on
insert** (the kernel assigns it) and carry the assigned order in dumps.
Replay re-inserts stored records whose order field is non-zero; that is
an internal path and the kernel overwrites the order (validation of the
zero-order rule happens only at the external ABI boundary).

Enums: `border_style` 0 solid / 1 dashed; `inset`/`wavy`/`is_start`/the
`smoothed` command field 0/1; `command_kind` 0 batch / 1 begin-filter /
2 end-filter / 3 begin-effect / 4 end-effect (rev 16: the effect
commands carry the effect record index in `boundary_index`, the closing
marker's index in `closing_boundary_index`, and the pool target slot in
`filter_target`/`target_index` like the filter commands);
`primitive_kind` 0 shadows, 1 quads, 2 paths, 3 underlines, 4
monochrome sprites, 5 subpixel sprites, 6 polychrome sprites, 7
surfaces, 8 backdrop filters, 9 filter boundary, 10 effect boundary
(rev 16; never a batch command - markers compile to begin/end effect
commands); `filter_target` 0 inline / 1 isolated (with the pool target
slot in `target_index`; rev 16 assigns nesting slots - sibling groups
reuse one slot, and scenes nested at most two deep produce exactly the
R15 plan).

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
| 27 | `insert_effect` | `(u64, *const GpuiGoSceneEffectRecord) -> i32` (version 6, rev 16: the effect-group marker insert; the record's `flags.IS_START` decides start/end; the batch ops 12/13 carry the same payload) |
| 28 | `effect_dump` | `(u64, *mut GpuiGoSceneEffectRecord, u32 capacity, *mut u32 out_count) -> i32` (version 6: the finished scene's effect records with kernel-assigned orders; the plan_dump capacity protocol) |
| 29 | `update_property` | `(u64 scene, u32 node_id, u32 kind /* 0 matrix, 1 opacity */, *const u32 values, u32 value_count, *mut GpuiGoSceneBounds out_old, *mut GpuiGoSceneBounds out_new) -> i32` (version 7, rev 16 ticket 51: mutates the finished scene's property-node effect records in place - no re-sort, no re-plan - and reports the old/new mapped device bounds; the design's appendix A.7) |
| 30 | `insert_gradient` | `(u64, *const GpuiGoSceneGradientRecord, *const GpuiGoSceneGradientStop, u32 stop_count, *mut u32 out_index) -> i32` (version 8, rev 16 ticket 52: one gradient table entry + its bulk stop array; the service bakes the 1024x1 premultiplied-sRGB ramp row and assigns stop_row; out_index receives the table index, null allowed) |
| 31 | `gradient_dump` | `(u64, *mut GpuiGoSceneGradientRecord, *mut GpuiGoSceneGradientStop, u32 record_capacity, u32 stop_capacity, *mut u32 out_record_count, *mut u32 out_stop_count) -> i32` (version 8: the finished scene's gradient records + the concatenated stops; the path_dump capacity protocol) |
| 32 | `insert_coverage` | `(u64, *const GpuiGoSceneCoverageRecord, *mut u32 out_index) -> i32` (version 8: one clip-path coverage entry - a path reference + device bounds; out_index receives the coverage index, null allowed) |
| 33 | `insert_filter_op_chain` | `(u64, *const GpuiGoSceneFilterOpRecord, u32 op_count, *mut u32 out_index) -> i32` (version 8: one drop-shadow op chain; out_index receives the chain index, null allowed) |

Self-check scalars: `service_version` (8), `size_of_table` (376),
`align_of_table` (8), `size_of_quad_record` (280),
`size_of_shadow_record` (216), `size_of_underline_record` (112),
`size_of_filter_record` (820), `size_of_surface_record` (96),
`size_of_monochrome_sprite_record` (164),
`size_of_polychrome_sprite_record` (144),
`size_of_command_record` (56), `size_of_requirements_record` (48),
`size_of_meta_record` (60), `size_of_path_record` (156),
`size_of_path_vertex_record` (16), `size_of_path_command_record` (56),
`max_filters` (8), `size_of_effect_record` (116),
`size_of_content_mask` (56), `max_scene_groups` (1024), `size_of_gradient_record` (56), `size_of_gradient_stop` (8), `max_gradient_stops` (64), `size_of_coverage_record` (24), `size_of_filter_op_record` (40) (rev 16, ticket 52).

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

Rev 16 (ticket 50): `MAX_SCENE_GROUPS` = 1024 bounds the group START
markers (filter boundaries + effect groups together - one group stack);
inserting the start that would exceed it returns `ERR_LIMIT`. The
pinned `MAX_FILTER_GROUP_DEPTH` = 2 is GONE: the plan gives every
matched group of either kind an isolated pool target, and the
renderer's per-surface group-target pool (64 targets / 256 MB, see
RENDERER_ABI.md's `surface_debug_stats`) is the only demotion point. A
demoted group renders inline (un-grouped, unfiltered), observably: the
pool's overflow counter and a debug-build warning.

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
