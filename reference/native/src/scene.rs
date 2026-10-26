//! gpui-go native scene kernel service (ticket08, reserved slot 3).
//!
//! Implements the scene ABI documented in [`SCENE_ABI.md`]: a registry of
//! scene kernels behind `#[repr(C)]` records and `extern "system"`
//! function tables. The service table is installed in **reserved slot 3**
//! of the bootstrap `GpuiGoAbiTable` and advertised with capability bit 3
//! (`scene-kernel-v1`).
//!
//! Semantics follow the pinned GPUI-CE scene kernel exactly
//! (`crates/gpui/src/scene.rs`, `crates/gpui/src/scene/plan.rs` and
//! `crates/gpui/src/bounds_tree.rs` at commit `254b5dbd…`):
//!
//! * `insert` computes the draw order from the bounds tree: one greater
//!   than the maximum order of any intersecting bounds, raised to the
//!   order floor when one is set (`BoundsTree::insert`); a primitive
//!   painted inside a layer carries the LAYER's order (the pinned
//!   `layer_stack.last()` fallback), so a layer is a batch of geometry
//!   sharing one draw order;
//! * a primitive whose clipped bounds are empty is dropped, EXCEPT
//!   content-filter boundaries, which always insert (matched pairs must
//!   survive clipping) and take an order strictly above all prior content
//!   (`BoundsTree::insert_above_all`); the END marker additionally raises
//!   the order floor above itself so later non-overlapping content cannot
//!   fall back inside the group's order range;
//! * `raise_order_floor` (the deferred-draw floor: overlays and their
//!   backdrops sort above the main scene) sets the tree's floor to
//!   `max_order() + 1`;
//! * `finish` stable-sorts each primitive array by draw order (the
//!   boundary arrays additionally by `!is_start` so a degenerate
//!   start/end tie keeps the pair well-formed), pairs surfaces with their
//!   opacities through the sort, and compiles the render plan;
//! * the plan (the pinned `ScenePlan::build` + `BatchIterator`) merges
//!   runs of one primitive kind into batches split by corner smoothing,
//!   emits content-filter boundaries and effect-group markers one at a
//!   time, pairs nested groups of either kind on ONE shared stack
//!   (rev 16: no depth cap - every matched group gets a pool target
//!   slot; the renderer's pool budget is the demotion point) and
//!   collects the resource requirements;
//! * `replay` re-inserts a range of a previous scene's paint operations
//!   into the target scene (orders recomputed against the TARGET's
//!   bounds tree and layer state; surfaces keep their paired opacities).
//!
//! Supported primitive classes: `Quad`, `Shadow`, `Underline`,
//! `BackdropFilter`, `FilterBoundary` and `Surface` (with paired
//! opacity), and — since ticket10 — the three sprite classes
//! `MonochromeSprite`, `SubpixelSprite` and `PolychromeSprite` (atlas
//! tiles drawn as textured quads; the pinned scene's `sort_by_key(|s|
//! (s.order, s.tile.tile_id))` finish tie-break and its per-texture
//! batch splitting). Paths are later work; their batch/command kinds
//! remain reserved and never emitted, and the primitive-kind tie-break
//! order (which decides which kind draws first at an equal order) is the
//! pinned `PrimitiveKind` discriminant order, so they cannot reorder
//! existing scenes.
//!
//! # Contract highlights
//!
//! * Every entry point contains panics with `catch_unwind` and returns a
//!   status; a panicking export marks the scene failed
//!   ([`scene_status::ERR_PANIC`], then [`scene_status::ERR_SCENE_FAILED`]
//!   for subsequent calls until clear/dispose).
//! * Scene handles encode `(slot index << 20) | slot generation`. A scene
//!   handle is stale after dispose (or slot reuse). Stale use returns
//!   [`scene_status::ERR_STALE_HANDLE`]; a malformed handle returns
//!   [`scene_status::ERR_BAD_HANDLE`].
//! * Every insert validates the whole record first (record-size
//!   self-check, zero order field, supported tags/enumerants, filter
//!   count bound); a malformed record returns
//!   [`scene_status::ERR_BAD_VALUE`] without publishing anything.
//! * Capacity bounds ([`MAX_SCENE_OPERATIONS`],
//!   [`MAX_SCENE_PRIMITIVES`], [`MAX_SCENE_FILTERS`],
//!   [`MAX_SCENE_PLAN_COMMANDS`]) bound every array; exceeding them
//!   returns [`scene_status::ERR_LIMIT`] without partial publication.
//! * Dumps copy the finished scene's records into caller buffers (bulk
//!   dump); a too-small capacity returns [`scene_status::ERR_CAPACITY`]
//!   with the required count in `out_count`.
//! * No Go pointers are retained; all records are copied into native
//!   storage at insert and back into caller buffers at dump.
//!
//! [`SCENE_ABI.md`]: ../SCENE_ABI.md

use std::panic::{catch_unwind, AssertUnwindSafe};
use std::sync::atomic::{AtomicBool, AtomicUsize, Ordering};
use std::sync::{Arc, Mutex, MutexGuard};

use gpui::{Path as GpuiPath, PathBuilder, PathStyle, Pixels, point, px};
use lyon::tessellation::{FillOptions, FillRule, LineCap, LineJoin, StrokeOptions};
use lyon::math::Transform as LyonTransform;

// ---------------------------------------------------------------------------
// Constants and status codes
// ---------------------------------------------------------------------------

/// Scene service ABI version (independent of the bootstrap table's
/// `abi_version`; bumped only on scene-record/table layout changes).
/// Version 3 (ticket16) added the path primitive class, the path dump
/// and the pinned-PathBuilder tessellation entry.
/// Version 4 (gpui-perf ticket 05) appended the `insert_batch` entry
/// (one packed op stream per call); the table grew to 27 pointers, size
/// 288.
/// Version 5 (rev 15.1, browser-core ticket 77) grew the filter
/// record from 188 to 780 bytes (`MAX_SCENE_FILTERS` 4 → 8;
/// `color_matrix` 20 → 160 words, one slot per colour function with
/// the [0,1] clamp between — the rev-15 composed payload could not
/// express Chrome's per-function clamping); no table slots changed.
/// Version 6 (rev 16, browser-core ticket 50) added the effect-group
/// primitive (insert/effect_dump, batch ops 12/13, plan kinds 3/4, the
/// 52-byte content mask, the single-primitive fold, the meta
/// effect_count and the requirements isolated_effect_count).
/// Version 7 (rev 16, browser-core ticket 51) appended `update_property`
/// (30 pointers, 320 bytes).
/// Version 8 (rev 16, browser-core ticket 52) appended the scene-level
/// tables the effect groups ride: `scene_insert_gradient` /
/// `scene_gradient_dump` (the gradient table with its baked 1024x1
/// premultiplied-sRGB ramp rows, up to 64 stops),
/// `scene_insert_coverage` (the clip-path coverage entries) and
/// `scene_insert_filter_op_chain` (the R15-append drop-shadow ops) -
/// 34 pointers, 376 bytes; the effect record's mask kinds 1-3 and the
/// `has_blend` composite are wired (the renderer resolves them); the
/// filter record appends `op_chain_index`/`op_chain_count` (0/0 = R15
/// semantics, bit-exact) and the meta record appends the table counts.
pub const GPUI_GO_SCENE_SERVICE_VERSION: u32 = 8;

/// Property kinds of `scene_update_property` (rev 16, ticket 51: the
/// design's appendix A.7). A kind updates the records' stored value
/// in place - no re-sort, no re-plan (orders and indices never move).
pub mod scene_property_kind {
    /// The 2x3 transform matrix: six f32 values, row-major
    /// `[m00, m01, m10, m11, tx, ty]` (the record's `transform`
    /// layout; requires an invertible 2x2, like the insert rule).
    /// Requires the node's records to carry `has_transform`.
    pub const MATRIX: u32 = 0;
    /// The group opacity: one f32 value in [0, 1]. Requires the node's
    /// records to carry `has_opacity`.
    pub const OPACITY: u32 = 1;
}

/// Scene service status codes. 0 is success, negative values are
/// caller/argument errors, 100+ are internal failures.
pub mod scene_status {
    /// Success.
    pub const OK: i32 = 0;
    /// The handle encoded a past generation (scene disposed or slot
    /// reused since).
    pub const ERR_STALE_HANDLE: i32 = -1;
    /// The handle never existed or is malformed (out-of-range slot).
    pub const ERR_BAD_HANDLE: i32 = -2;
    /// A required pointer argument was null.
    pub const ERR_NULL_ARG: i32 = -3;
    /// A record field held an invalid tag, enumerant, bound or size
    /// (record-size self-check mismatch, non-zero order field on insert,
    /// unsupported color tag, unknown dump kind, arithmetic overflow).
    pub const ERR_BAD_VALUE: i32 = -4;
    /// A plan/requirements/dump read was attempted before `scene_finish`.
    pub const ERR_NOT_FINISHED: i32 = -5;
    /// `scene_dump`/`scene_plan_dump` capacity was smaller than the
    /// content; `out_count` carries the required count.
    pub const ERR_CAPACITY: i32 = -6;
    /// The scene is marked failed (a contained panic occurred); clear or
    /// dispose is required.
    pub const ERR_SCENE_FAILED: i32 = -7;
    /// An insert exceeded a scene capacity bound (operations, primitives,
    /// filters or plan commands).
    pub const ERR_LIMIT: i32 = -8;
    /// A panic was contained by `catch_unwind` during this call; the
    /// scene is now marked failed.
    pub const ERR_PANIC: i32 = 101;
}

/// Maximum paint operations in one scene's log (the replay address space).
pub const MAX_SCENE_OPERATIONS: u32 = 262_144;
/// Maximum primitives of one class in one scene.
pub const MAX_SCENE_PRIMITIVES: u32 = 65_536;
/// Maximum filters in one filter chain (rev 15.1 grew it from the pin's
/// `SmallVec<[ScaledFilter; 4]>` inline capacity 4 to 8 so a chain can
/// carry the per-function colour matrices — one entry each — beside
/// the composed blur; a longer chain composes its excess, see
/// `GpuiGoSceneFilterRecord`).
pub const MAX_SCENE_FILTERS: u32 = 8;
/// Maximum compiled plan commands in one scene.
pub const MAX_SCENE_PLAN_COMMANDS: u32 = 262_144;

/// Maximum vertices in one inserted path (an ABI memory bound on the
/// bulk vertex array of `insert_path`; the pinned scene imposes no
/// explicit per-path bound, but the ABI validates every bulk copy).
pub const MAX_SCENE_PATH_VERTICES: u32 = 1_048_576;

/// Maximum gradients in one scene's gradient table (rev 16, ticket 52;
/// each entry carries its stops and the baked 1024x1 ramp row, so the
/// bound also caps the ramp texture's height and the table's bytes).
pub const MAX_SCENE_GRADIENTS: u32 = 256;

/// Maximum stops in one gradient (rev 16, ticket 52; the user's 2026-
/// 10-24 decision - the design text's 32 was raised to 64. Above it the
/// GO layer resamples down to 64 and warns in debug builds; the native
/// service rejects the insert with `ERR_LIMIT`).
pub const MAX_GRADIENT_STOPS: u32 = 64;

/// The baked ramp row's texel width (rev 16, ticket 52): every
/// gradient's stops are baked into one 1024x1 premultiplied sRGB row
/// (CSS's interpolation rule is linear in that space, so the hardware's
/// linear filtering of the baked bytes IS the interpolation).
pub const GRADIENT_RAMP_WIDTH: u32 = 1024;

/// Maximum coverage-mask entries in one scene (rev 16, ticket 52; the
/// clip-path A8 rasterization sources).
pub const MAX_SCENE_COVERAGE: u32 = 256;

/// Maximum filter-op chains in one scene (rev 16, ticket 52: the
/// drop-shadow chains the R15 filter records reference).
pub const MAX_SCENE_FILTER_OP_CHAINS: u32 = 256;

/// Maximum ops in one filter-op chain (rev 16, ticket 52).
pub const MAX_SCENE_FILTER_OPS: u32 = 8;

/// **Removed in rev 16 (browser-core ticket 50):** the pinned
/// `MAX_FILTER_GROUP_DEPTH = 2` cap (deeper groups rendered inline,
/// unfiltered) is gone - the R16 target pool has no depth cap, every
/// matched group of either kind (filter boundary or effect group) gets
/// an isolated pool target, and the renderer's per-surface pool budget
/// is the only demotion point (a demoted group renders inline,
/// observably: a debug-stats counter and a debug-build warning). The
/// ABI sanity bound is now [`MAX_SCENE_GROUPS`].

/// Maximum group START markers (filter boundaries + effect groups) in
/// one scene - an ABI sanity bound, not a semantic cap (rev 16,
/// ticket 50). Inserting the start marker that would exceed it
/// returns [`scene_status::ERR_LIMIT`]; the renderer's per-surface
/// pool budget is the real cap.
pub const MAX_SCENE_GROUPS: u32 = 1024;

// ---------------------------------------------------------------------------
// ABI records
// ---------------------------------------------------------------------------

/// Scene bounds record: `x`, `y`, `w`, `h` as IEEE-754 f32 bits (device
/// pixels). 16 bytes, alignment 4.
#[repr(C)]
#[derive(Clone, Copy, PartialEq, Debug, Default)]
pub struct GpuiGoSceneBounds {
    pub x: u32,
    pub y: u32,
    pub w: u32,
    pub h: u32,
}

/// The extended content mask (rev 16, browser-core ticket 50): the pin's
/// shader-side `ContentMask` (bounds plus the linear per-edge `fade_out`
/// ramp, `shaders/common.rs`'s `ContentMask::alpha`) finally carried by
/// the ABI, plus the rounded-clip fields (corner radii and smoothing -
/// the innermost rounded clip applies INLINE in every vendored pipeline
/// through the rounded-rectangle SDF; ancestors' rounded clips become
/// effect groups, see `GpuiGoSceneEffectRecord`). 13 words = 52 bytes,
/// alignment 4; replaces the 16-byte bounds-only mask in every record
/// that has one.
///
/// `fade_out` word order is LEFT, TOP, RIGHT, BOTTOM (each f32 bits, in
/// DEVICE pixels): a zero distance leaves that edge fully opaque, and
/// the ramp runs from the mask bound inward, exactly the pin's
/// `ContentMask::alpha`. `corner_radii` are top-left, top-right,
/// bottom-right, bottom-left (f32 bits, device pixels); a zero-radius
/// mask clips rect-only (the record's pre-R16 behaviour).
#[repr(C)]
#[derive(Clone, Copy, PartialEq, Debug, Default)]
pub struct GpuiGoSceneContentMask {
    /// The clip bounds in device pixels (the rect intersection).
    pub bounds: GpuiGoSceneBounds,
    /// Per-edge fade distances: left, top, right, bottom (f32 bits).
    pub fade_out: [u32; 4],
    /// Rounded-corner clip radii: top-left, top-right, bottom-right,
    /// bottom-left (f32 bits).
    pub corner_radii: [u32; 4],
    /// Rounded-corner smoothing in [0, 1] (f32 bits); carried for the
    /// rounded clip (the vendored pipelines evaluate the plain rrect
    /// SDF this revision; the smoothed-corner SDF rides the record's
    /// own smoothing where the pipelines already support it).
    pub corner_smoothing: u32,
    /// Layout pad (the mask's HLSL structured-buffer struct size rounds
    /// up to its 8-byte alignment - the mask's bounds member forces
    /// it); must be zero.
    pub mask_padding: u32,
}

impl GpuiGoSceneContentMask {
    /// The mask's corner radii as f32 (validation and snapshot use).
    pub fn radii_f32(&self) -> [f32; 4] {
        [
            f32::from_bits(self.corner_radii[0]),
            f32::from_bits(self.corner_radii[1]),
            f32::from_bits(self.corner_radii[2]),
            f32::from_bits(self.corner_radii[3]),
        ]
    }

    /// True when the mask clips with rounded corners (any non-zero
    /// radius). A rect-only mask behaves exactly as before rev 16.
    pub fn is_rounded(&self) -> bool {
        self.corner_radii.iter().any(|bits| *bits != 0)
    }
}

/// Scene color record: `tag` plus the HSL-with-alpha components as f32
/// bits. Tag 0 is the only supported value in this revision (the pinned
/// `Background`'s solid form, `BackgroundTag::Solid`); gradient and
/// pattern tags are reserved. 20 bytes, alignment 4.
#[repr(C)]
#[derive(Clone, Copy, PartialEq, Debug)]
pub struct GpuiGoSceneColor {
    /// 0 = solid.
    pub tag: u32,
    /// Hue in `[0, 1]` (degrees / 360), f32 bits.
    pub h: u32,
    /// Saturation in `[0, 1]`, f32 bits.
    pub s: u32,
    /// Lightness in `[0, 1]`, f32 bits.
    pub l: u32,
    /// Alpha in `[0, 1]`, f32 bits.
    pub a: u32,
}

impl Default for GpuiGoSceneColor {
    fn default() -> Self {
        // The pinned transparent black: h 0, s 0, l 0, a 0.
        GpuiGoSceneColor { tag: 0, h: 0, s: 0, l: 0, a: 0 }
    }
}

/// Scene background record (browser-core ticket 39, rev 15): the full
/// mirror of the pinned `Background` (`crates/gpui/src/color.rs`,
/// `BackgroundTag::Solid` and `BackgroundTag::LinearGradient` with a
/// two-stop gradient and the interpolation color space). The quad's
/// `background`/`border_color`, the shadow's `color` and the path's
/// `color` carry it (the pinned structs hold a `Background` there);
/// the underline and sprite colors stay plain HSLA records
/// (`GpuiGoSceneColor`).
///
/// Tag 1 (linear gradient) is evaluated with CSS semantics by the
/// vendored shader: the gradient-line length is `|W·sin θ| +
/// |H·cos θ|` (the corners receive the end colors) and the stops
/// interpolate premultiplied in sRGB (the default, tag 0 of the color
/// space) — the two recorded rev-15 divergences from the pinned
/// shader. The angle follows the pinned convention (CSS degrees, 0
/// pointing up, growing clockwise; 90 is to the right).
///
/// The R16 gradient-table background tag (rev 16, ticket 52): the
/// background references a scene-level gradient table entry by index
/// (the index stored in the record's `angle` field as f32 bits); the
/// quad/shadow pipelines evaluate the table gradient per pixel. R15's
/// tags 0-3 keep their exact meaning.
pub const SCENE_BACKGROUND_TAG_GRADIENT_TABLE: u32 = 4;

/// Layout (x86-64, `#[repr(C)]`): `tag` @0, `color_space` @4, `h` @8,
/// `s` @12, `l` @16, `a` @20, `angle` @24, `stop0_h` @28, `stop0_s`
/// @32, `stop0_l` @36, `stop0_a` @40, `stop0_percentage` @44,
/// `stop1_h` @48, `stop1_s` @52, `stop1_l` @56, `stop1_a` @60,
/// `stop1_percentage` @64, `reserved` @68; size 72, alignment 4.
#[repr(C)]
#[derive(Clone, Copy, PartialEq, Debug)]
pub struct GpuiGoSceneBackground {
    /// 0 = solid, 1 = linear gradient (the pinned `BackgroundTag`).
    pub tag: u32,
    /// 0 = sRGB (the ABI default, CSS's legacy interpolation space),
    /// 1 = Oklab (the pinned `ColorSpace`).
    pub color_space: u32,
    /// Solid hue in `[0, 1]` (degrees / 360), f32 bits (tag 0).
    pub h: u32,
    /// Solid saturation in `[0, 1]`, f32 bits (tag 0).
    pub s: u32,
    /// Solid lightness in `[0, 1]`, f32 bits (tag 0).
    pub l: u32,
    /// Solid alpha in `[0, 1]`, f32 bits (tag 0).
    pub a: u32,
    /// Gradient angle in CSS degrees (f32 bits; 0 = up, clockwise,
    /// 90 = to the right; any finite value, taken modulo 360).
    pub angle: u32,
    /// Stop 0 hue in `[0, 1]`, f32 bits (tag 1).
    pub stop0_h: u32,
    /// Stop 0 saturation in `[0, 1]`, f32 bits (tag 1).
    pub stop0_s: u32,
    /// Stop 0 lightness in `[0, 1]`, f32 bits (tag 1).
    pub stop0_l: u32,
    /// Stop 0 alpha in `[0, 1]`, f32 bits (tag 1).
    pub stop0_a: u32,
    /// Stop 0 position along the gradient line (f32 bits; the CSS
    /// percentage / 100 — any finite value, 0 = the line's start, 1 =
    /// its end; a descending pair reverses the gradient).
    pub stop0_percentage: u32,
    /// Stop 1 hue in `[0, 1]`, f32 bits (tag 1).
    pub stop1_h: u32,
    /// Stop 1 saturation in `[0, 1]`, f32 bits (tag 1).
    pub stop1_s: u32,
    /// Stop 1 lightness in `[0, 1]`, f32 bits (tag 1).
    pub stop1_l: u32,
    /// Stop 1 alpha in `[0, 1]`, f32 bits (tag 1).
    pub stop1_a: u32,
    /// Stop 1 position along the gradient line (f32 bits; must differ
    /// from stop 0's — the shader divides by the delta, and CSS's
    /// equal-position hard stop needs the n-stop form).
    pub stop1_percentage: u32,
    /// Must be zero (forward compatibility; the pinned struct's
    /// padding word).
    pub reserved: u32,
}

impl Default for GpuiGoSceneBackground {
    fn default() -> Self {
        // The pinned Background::default(): the solid transparent black.
        GpuiGoSceneBackground {
            tag: 0,
            color_space: 0,
            h: 0,
            s: 0,
            l: 0,
            a: 0,
            angle: 0,
            stop0_h: 0,
            stop0_s: 0,
            stop0_l: 0,
            stop0_a: 0,
            stop0_percentage: 0,
            stop1_h: 0,
            stop1_s: 0,
            stop1_l: 0,
            stop1_a: 0,
            stop1_percentage: 0,
            reserved: 0,
        }
    }
}

impl GpuiGoSceneBackground {
    /// The solid HSLA arm (tag 0) as a color record.
    pub fn solid(color: GpuiGoSceneColor) -> Self {
        GpuiGoSceneBackground {
            tag: 0,
            color_space: 0,
            h: color.h,
            s: color.s,
            l: color.l,
            a: color.a,
            ..Default::default()
        }
    }
}

/// Quad record. Layout (x86-64, `#[repr(C)]`): `order` @0,
/// `border_style` @4, `border_dashed_length` @8, `border_dashed_gap`
/// @12, `bounds` @16, `content_mask` @32 (56 bytes, the rev-16 extended
/// mask), `background` @88, `border_color` @160, `corner_radii` @232,
/// `border_widths` @248, `corner_smoothing` @264, `padding` @268,
/// `layout_padding` @272, `record_size` @276; size 280, alignment 4.
///
/// rev 16 (ticket 50): the field order mirrors the PINNED struct - the
/// draw path uploads this record DIRECTLY as the vendored quad
/// pipeline's instance array, and the HLSL structured-buffer layout
/// aligns the vec2f-bearing fields (bounds, the mask's bounds) to 8
/// bytes; the pin's field order is the alignment-safe one (the
/// layout_padding/record_size pair pads the tail to the same rounded
/// size). The pre-R16 field semantics are unchanged.
///
/// `order` mirrors the pinned `Quad::order`: it must be 0 on insert (the
/// kernel assigns the draw order) and carries the assigned order in
/// dumps.
///
/// rev 15 (browser-core ticket 39): `background` and `border_color` are
/// full background records (solid or a two-stop linear gradient with CSS
/// semantics; the pinned shader's gradient support, previously dropped
/// by the ABI).
#[repr(C)]
#[derive(Clone, Copy, PartialEq, Debug)]
pub struct GpuiGoSceneQuadRecord {
    /// Draw order. Must be 0 on insert; kernel-assigned in dumps.
    pub order: u32,
    /// 0 = solid, 1 = dashed (pinned `BorderStyle`).
    pub border_style: u32,
    /// Dashed border dash length, f32 bits (multiple of border width).
    pub border_dashed_length: u32,
    /// Dashed border dash gap, f32 bits.
    pub border_dashed_gap: u32,
    /// Bounds in device pixels.
    pub bounds: GpuiGoSceneBounds,
    /// Content mask (the rev-16 extended mask: bounds, per-edge fade
    /// distances and the rounded-clip radii; a zero-radii, zero-fade
    /// mask behaves exactly as the pre-R16 bounds-only mask).
    pub content_mask: GpuiGoSceneContentMask,
    /// Background (solid or a two-stop linear gradient).
    pub background: GpuiGoSceneBackground,
    /// Border (solid or a two-stop linear gradient).
    pub border_color: GpuiGoSceneBackground,
    /// Corner radii (top-left, top-right, bottom-right, bottom-left), f32
    /// bits, device pixels.
    pub corner_radii: [u32; 4],
    /// Border widths (top, right, bottom, left), f32 bits, device pixels.
    pub border_widths: [u32; 4],
    /// Corner smoothing, f32 bits.
    pub corner_smoothing: u32,
    /// Padding (payload; the pinned field, always 0 from the paint path).
    pub padding: u32,
    /// Layout pad (the record's HLSL struct size rounds up to its
    /// 8-byte alignment); must be zero.
    pub layout_padding: u32,
    /// `size_of::<GpuiGoSceneQuadRecord>()` self-check.
    pub record_size: u32,
}

impl Default for GpuiGoSceneQuadRecord {
    fn default() -> Self {
        GpuiGoSceneQuadRecord {
            order: 0,
            border_style: 0,
            border_dashed_length: 0,
            border_dashed_gap: 0,
            bounds: GpuiGoSceneBounds::default(),
            content_mask: GpuiGoSceneContentMask::default(),
            background: GpuiGoSceneBackground::default(),
            border_color: GpuiGoSceneBackground::default(),
            corner_radii: [0; 4],
            border_widths: [0; 4],
            corner_smoothing: 0,
            padding: 0,
            layout_padding: 0,
            record_size: core::mem::size_of::<GpuiGoSceneQuadRecord>() as u32,
        }
    }
}

/// Shadow record. Layout: `order` @0, `blur_radius` @4, `bounds` @8,
/// `corner_radii` @24, `content_mask` @40 (56 bytes, the rev-16 extended
/// mask), `color` @96, `element_bounds` @168, `element_corner_radii`
/// @184, `inset` @200, `corner_smoothing` @204, `layout_padding` @208,
/// `record_size` @212; size 216, alignment 4. The field order (the
/// pin's) keeps every vec2-bearing field 8-byte aligned, and the
/// trailing pad reaches the HLSL structured-buffer size - this record
/// IS the vendored shadow pipeline's instance layout.
///
/// rev 15 (browser-core ticket 39): `color` is a full background record
/// (solid or a two-stop gradient), and drop shadows are clipped out of
/// the rounded `element_bounds` (CSS css-backgrounds-3 §box-shadow; a
/// recorded divergence from the pinned shader — see `shaders.rs`).
#[repr(C)]
#[derive(Clone, Copy, PartialEq, Debug)]
pub struct GpuiGoSceneShadowRecord {
    /// Draw order. Must be 0 on insert; kernel-assigned in dumps.
    pub order: u32,
    /// Blur radius, f32 bits, device pixels.
    pub blur_radius: u32,
    /// Shadow bounds (the dilated region), device pixels.
    pub bounds: GpuiGoSceneBounds,
    /// Corner radii of the shadow shape, f32 bits.
    pub corner_radii: [u32; 4],
    /// Content mask bounds.
    /// Content mask (the rev-16 extended mask: bounds, per-edge fade
    /// distances and the rounded-clip radii; a zero-radii, zero-fade
    /// mask behaves exactly as the pre-R16 bounds-only mask).
    pub content_mask: GpuiGoSceneContentMask,
    /// Shadow color (solid or a two-stop linear gradient).
    pub color: GpuiGoSceneBackground,
    /// The element's bounds (the shadow's un-dilated frame — the
    /// border box a drop shadow is clipped out of).
    pub element_bounds: GpuiGoSceneBounds,
    /// The element's corner radii, f32 bits.
    pub element_corner_radii: [u32; 4],
    /// 0 = drop shadow, 1 = inset (pinned `ShaderBool`).
    pub inset: u32,
    /// Corner smoothing, f32 bits.
    pub corner_smoothing: u32,
    /// Layout pad (the record's HLSL struct size rounds up to its
    /// 8-byte alignment); must be zero.
    pub layout_padding: u32,
    /// `size_of::<GpuiGoSceneShadowRecord>()` self-check.
    pub record_size: u32,
}

impl Default for GpuiGoSceneShadowRecord {
    fn default() -> Self {
        GpuiGoSceneShadowRecord {
            order: 0,
            blur_radius: 0,
            bounds: GpuiGoSceneBounds::default(),
            corner_radii: [0; 4],
            content_mask: GpuiGoSceneContentMask::default(),
            color: GpuiGoSceneBackground::default(),
            element_bounds: GpuiGoSceneBounds::default(),
            element_corner_radii: [0; 4],
            inset: 0,
            corner_smoothing: 0,
            layout_padding: 0,
            record_size: core::mem::size_of::<GpuiGoSceneShadowRecord>() as u32,
        }
    }
}

/// Underline record. Layout: `order` @0, `padding` @4, `bounds` @8,
/// `content_mask` @24 (52 bytes, the rev-16 extended mask), `color` @76,
/// `thickness` @96, `wavy` @100, `record_size` @104; size 108, alignment 4.
#[repr(C)]
#[derive(Clone, Copy, PartialEq, Debug)]
pub struct GpuiGoSceneUnderlineRecord {
    /// Draw order. Must be 0 on insert; kernel-assigned in dumps.
    pub order: u32,
    /// Padding (payload; 0 from the paint path).
    pub padding: u32,
    /// Bounds in device pixels.
    pub bounds: GpuiGoSceneBounds,
    /// Content mask bounds.
    /// Content mask (the rev-16 extended mask: bounds, per-edge fade
    /// distances and the rounded-clip radii; a zero-radii, zero-fade
    /// mask behaves exactly as the pre-R16 bounds-only mask).
    pub content_mask: GpuiGoSceneContentMask,
    /// Underline color.
    pub color: GpuiGoSceneColor,
    /// Stroke thickness, f32 bits, device pixels.
    pub thickness: u32,
    /// 0 = straight, 1 = wavy (pinned `ShaderBool`).
    pub wavy: u32,
    /// `size_of::<GpuiGoSceneUnderlineRecord>()` self-check.
    pub record_size: u32,
}

impl Default for GpuiGoSceneUnderlineRecord {
    fn default() -> Self {
        GpuiGoSceneUnderlineRecord {
            order: 0,
            padding: 0,
            bounds: GpuiGoSceneBounds::default(),
            content_mask: GpuiGoSceneContentMask::default(),
            color: GpuiGoSceneColor::default(),
            thickness: 0,
            wavy: 0,
            record_size: core::mem::size_of::<GpuiGoSceneUnderlineRecord>() as u32,
        }
    }
}

/// Filter record, shared by backdrop filters and content-filter
/// boundaries (the pinned `BackdropFilter`/`FilterBoundary` shapes; the
/// boundary adds `is_start`). Layout: `order` @0, `bounds` @4,
/// `content_mask` @20 (52 bytes, the rev-16 extended mask),
/// `corner_radii` @72, `corner_smoothing` @88, `blur_radii` @92,
/// `filter_count` @124, `opacity` @128, `is_start` @132, `filter_tags`
/// @136, `color_matrix` @168, `color_matrix_count` @808, `record_size`
/// @812, `op_chain_index` @816, `op_chain_count` @820; size 828,
/// alignment 4.
///
/// Filters are gaussian blurs with radii in device pixels (the pinned
/// `ScaledFilter::Blur`) or colour-matrix filters (rev 15, browser-core
/// ticket 39: CSS filter-effects' colour functions are 4x5 matrices -
/// grayscale, sepia, brightness, contrast, invert, saturate,
/// hue-rotate, opacity); up to [`MAX_SCENE_FILTERS`] entries with unused
/// entries zero. The per-filter tags say which; blur entries read their
/// radius from `blur_radii` in chain order, and each tag-1 entry maps to
/// one 20-word slot of `color_matrix` (rev 15.1, browser-core ticket 77:
/// `color_matrix` holds the chain's matrices ONE PER COLOUR FUNCTION in
/// chain order — slot i, words `20*i..20*i+20`, is the row-major 4x5
/// matrix of the i-th matrix filter — NOT a composed product, because
/// Chrome clamps each function's output to [0,1] before the next
/// applies, which one linear map cannot express; a chain longer than
/// the record's capacity composes its excess into the last slot, the
/// documented approximation). A per-pixel linear map commutes with the
/// uniform-per-channel gaussian, so blur-then-matrices (the draw path's
/// order) preserves the CSS chain order up to the clamps between a
/// blur and a matrix (the recorded approximation above).
#[repr(C)]
#[derive(Clone, Copy, PartialEq, Debug)]
pub struct GpuiGoSceneFilterRecord {
    /// Draw order. Must be 0 on insert; kernel-assigned in dumps.
    pub order: u32,
    /// Bounds in device pixels.
    pub bounds: GpuiGoSceneBounds,
    /// Content mask bounds.
    /// Content mask (the rev-16 extended mask: bounds, per-edge fade
    /// distances and the rounded-clip radii; a zero-radii, zero-fade
    /// mask behaves exactly as the pre-R16 bounds-only mask).
    pub content_mask: GpuiGoSceneContentMask,
    /// Corner radii, f32 bits.
    pub corner_radii: [u32; 4],
    /// Corner smoothing, f32 bits.
    pub corner_smoothing: u32,
    /// Blur radii (device pixels, f32 bits), first `filter_count` entries.
    pub blur_radii: [u32; 8],
    /// Number of filters in the chain, 0..=8.
    pub filter_count: u32,
    /// Group opacity, f32 bits (element opacity captured at paint time).
    pub opacity: u32,
    /// Boundary marker: 1 = start (opens the group), 0 = end. Backdrop
    /// inserts pass 0.
    pub is_start: u32,
    /// Per-filter kind tags, first `filter_count` entries: 0 = blur, 1 =
    /// colour matrix (rev 15). Entries at and beyond `filter_count`
    /// must be zero.
    pub filter_tags: [u32; 8],
    /// The chain's colour matrices, one 20-word slot per matrix filter
    /// in chain order (rev 15.1): slot i is words `20*i..20*i+20`, the
    /// row-major 4x5 matrix of the i-th colour function — row 0 is
    /// `color_matrix[20*i..20*i+5]` (r' = m00*r + m01*g + m02*b +
    /// m03*a + m04), and so on. Slots at and beyond
    /// `color_matrix_count` must be zero.
    pub color_matrix: [u32; 160],
    /// How many of the chain's filters are colour matrices (must equal
    /// the number of tag-1 entries; 0 demands zeroed matrix words).
    pub color_matrix_count: u32,
    /// `size_of::<GpuiGoSceneFilterRecord>()` self-check.
    pub record_size: u32,
    /// The appended drop-shadow op-chain reference (rev 16, ticket 52,
    /// the design's appendix A.5): the chain index of the scene-level
    /// op array the group's DROP-SHADOW ops live in. Appended AFTER
    /// `record_size` (the size self-check catches old mirrors); 0/0
    /// (the default) is the R15 legacy chain - bit-exact, nothing
    /// changes for every existing record.
    pub op_chain_index: u32,
    /// The op count (0 = R15 legacy; the referenced chain is read up to
    /// this many entries).
    pub op_chain_count: u32,
}

impl GpuiGoSceneFilterRecord {
    /// The chain's maximum blur radius over its blur-tagged entries (the
    /// pinned `max_blur_radius`; colour-matrix entries contribute none).
    pub fn max_blur_radius(&self) -> f32 {
        let mut radius: f32 = 0.0;
        for index in 0..self.filter_count.min(MAX_SCENE_FILTERS) as usize {
            if self.filter_tags[index] == 0 {
                radius = radius.max(f32::from_bits(self.blur_radii[index]));
            }
        }
        radius
    }
}

impl Default for GpuiGoSceneFilterRecord {
    fn default() -> Self {
        GpuiGoSceneFilterRecord {
            order: 0,
            bounds: GpuiGoSceneBounds::default(),
            content_mask: GpuiGoSceneContentMask::default(),
            corner_radii: [0; 4],
            corner_smoothing: 0,
            blur_radii: [0; 8],
            filter_count: 0,
            opacity: 0,
            is_start: 0,
            filter_tags: [0; 8],
            color_matrix: [0; 160],
            color_matrix_count: 0,
            record_size: core::mem::size_of::<GpuiGoSceneFilterRecord>() as u32,
            op_chain_index: 0,
            op_chain_count: 0,
        }
    }
}

/// Surface source kind tags (the ABI's `SurfaceSource` variants).
pub mod surface_source_kind {
    /// The pinned `SurfaceSource::Unsupported` stand-in (the pinned
    /// renderer's draw path bails on it explicitly).
    pub const UNSUPPORTED: u32 = 0;
    /// A native capture frame handle in `source_reserved[0]` (the
    /// pinned `SurfaceSource::WindowsCapture` import — the renderer
    /// service resolves it through the capture service's frame
    /// registry; ticket28).
    pub const CAPTURE_FRAME: u32 = 1;
}

/// Surface record. Layout: `order` @0, `bounds` @4, `content_mask` @20
/// (52 bytes, the rev-16 extended mask), `source_tag` @72,
/// `source_reserved` @76, `record_size` @88; size 92, alignment 4.
///
/// The source is opaque payload (tag 0 = the pinned
/// `SurfaceSource::Unsupported` stand-in used by tests; tag 1 = a
/// capture frame handle in `source_reserved[0]`, the pinned
/// `SurfaceSource::WindowsCapture` import — ticket28); the paired
/// opacity is carried by `scene_insert_surface`'s `opacity_bits` argument
/// and by the parallel array of the surface dump.
#[repr(C)]
#[derive(Clone, Copy, PartialEq, Debug)]
pub struct GpuiGoSceneSurfaceRecord {
    /// Draw order. Must be 0 on insert; kernel-assigned in dumps.
    pub order: u32,
    /// Bounds in device pixels.
    pub bounds: GpuiGoSceneBounds,
    /// Content mask bounds.
    /// Content mask (the rev-16 extended mask: bounds, per-edge fade
    /// distances and the rounded-clip radii; a zero-radii, zero-fade
    /// mask behaves exactly as the pre-R16 bounds-only mask).
    pub content_mask: GpuiGoSceneContentMask,
    /// Source kind tag (payload; 0 = none/unsupported, 1 = a capture
    /// frame handle packed across `source_reserved[0]` (low 32) and
    /// `source_reserved[1]` (high 32)).
    pub source_tag: u32,
    /// Reserved source payload; must be zero.
    pub source_reserved: [u32; 3],
    /// `size_of::<GpuiGoSceneSurfaceRecord>()` self-check.
    pub record_size: u32,
}

impl Default for GpuiGoSceneSurfaceRecord {
    fn default() -> Self {
        GpuiGoSceneSurfaceRecord {
            order: 0,
            bounds: GpuiGoSceneBounds::default(),
            content_mask: GpuiGoSceneContentMask::default(),
            source_tag: 0,
            source_reserved: [0; 3],
            record_size: core::mem::size_of::<GpuiGoSceneSurfaceRecord>() as u32,
        }
    }
}

/// Atlas tile sub-record of the sprite records (the pinned `AtlasTile`,
/// `crates/gpui/src/platform.rs:1918`: texture id, tile id, padding and
/// the tile bounds inside the atlas texture). 32 bytes, alignment 4.
#[repr(C)]
#[derive(Clone, Copy, PartialEq, Debug)]
pub struct GpuiGoSceneTileRecord {
    /// The index of this tile's texture within its kind pool (the pinned
    /// `AtlasTextureId::index`).
    pub texture_index: u32,
    /// The texture kind tag (0 monochrome, 1 polychrome, 2 subpixel; the
    /// pinned `AtlasTextureId::kind`).
    pub texture_kind: u32,
    /// The tile id within its texture (the serialized etagere AllocId;
    /// the pinned `TileId`).
    pub tile_id: u32,
    /// Padding around the tile content in pixels.
    pub padding: u32,
    /// Tile bounds origin x within the texture.
    pub bounds_x: i32,
    /// Tile bounds origin y within the texture.
    pub bounds_y: i32,
    /// Tile bounds width.
    pub bounds_w: i32,
    /// Tile bounds height.
    pub bounds_h: i32,
}

impl Default for GpuiGoSceneTileRecord {
    fn default() -> Self {
        GpuiGoSceneTileRecord {
            texture_index: 0,
            texture_kind: 0,
            tile_id: 0,
            padding: 0,
            bounds_x: 0,
            bounds_y: 0,
            bounds_w: 0,
            bounds_h: 0,
        }
    }
}

impl GpuiGoSceneTileRecord {
    /// The texture-kind tag that matches the sprite class inserting this
    /// tile (the pinned `AtlasTextureKind` mapping of the sprite kinds:
    /// monochrome sprites live in R8 pools, subpixel and polychrome
    /// sprites in BGRA pools).
    fn expected_texture_kind(&self, class: SpriteClass) -> u32 {
        match class {
            SpriteClass::Monochrome => 0,
            SpriteClass::Subpixel => 2,
            SpriteClass::Polychrome => 1,
        }
    }
}

/// The sprite class an insert entry targets (the ABI has one insert
/// entry per pinned sprite struct).
#[derive(Clone, Copy, PartialEq, Debug)]
enum SpriteClass {
    Monochrome,
    Subpixel,
    Polychrome,
}

/// Monochrome/subpixel sprite record (the pinned `MonochromeSprite` and
/// `SubpixelSprite`, which share this exact field set:
/// `crates/gpui/src/scene.rs:1069-1104`). Layout: `order` @0, `padding`
/// @4, `bounds` @8, `content_mask` @24 (52 bytes, the rev-16 extended
/// mask), `color` @76, `tile` @96, `transformation` @128 (rotation-scale
/// 2x2 then translation xy, f32 bits), `pixelated` @152, `record_size`
/// @156; size 160, alignment 4.
///
/// rev 15 (browser-core ticket 39): `pixelated` selects nearest-neighbour
/// sampling for this sprite (CSS `image-rendering: pixelated`); the plan
/// splits sprite batches on it so a batch draws through one sampler.
#[repr(C)]
#[derive(Clone, Copy, PartialEq, Debug)]
pub struct GpuiGoSceneSpriteRecord {
    /// Draw order. Must be 0 on insert; kernel-assigned in dumps.
    pub order: u32,
    /// Padding (payload; 0 from the glyph paint path).
    pub padding: u32,
    /// Bounds in device pixels (the sprite's on-screen placement).
    pub bounds: GpuiGoSceneBounds,
    /// Content mask bounds.
    /// Content mask (the rev-16 extended mask: bounds, per-edge fade
    /// distances and the rounded-clip radii; a zero-radii, zero-fade
    /// mask behaves exactly as the pre-R16 bounds-only mask).
    pub content_mask: GpuiGoSceneContentMask,
    /// The mask color (monochrome coverage tint / subpixel blend color).
    pub color: GpuiGoSceneColor,
    /// The atlas tile this sprite samples.
    pub tile: GpuiGoSceneTileRecord,
    /// The pinned `TransformationMatrix` fields as f32 bits:
    /// rotation_scale[0][0], [0][1], [1][0], [1][1], translation[0],
    /// translation[1].
    pub transformation: [u32; 6],
    /// 0 = bilinear sampling (the pinned default), 1 = point sampling
    /// (CSS `image-rendering: pixelated`; rev 15).
    pub pixelated: u32,
    /// `size_of::<GpuiGoSceneSpriteRecord>()` self-check.
    pub record_size: u32,
}

impl Default for GpuiGoSceneSpriteRecord {
    fn default() -> Self {
        GpuiGoSceneSpriteRecord {
            order: 0,
            padding: 0,
            bounds: GpuiGoSceneBounds::default(),
            content_mask: GpuiGoSceneContentMask::default(),
            color: GpuiGoSceneColor::default(),
            tile: GpuiGoSceneTileRecord::default(),
            // The pinned `TransformationMatrix::unit()`.
            transformation: [
                1.0f32.to_bits(),
                0.0f32.to_bits(),
                0.0f32.to_bits(),
                1.0f32.to_bits(),
                0.0f32.to_bits(),
                0.0f32.to_bits(),
            ],
            pixelated: 0,
            record_size: core::mem::size_of::<GpuiGoSceneSpriteRecord>() as u32,
        }
    }
}

/// Polychrome sprite record (the pinned `PolychromeSprite`,
/// `crates/gpui/src/scene.rs:1107-1121`: no color, no transformation;
/// grayscale flag, opacity, corner radii and smoothing instead).
/// Layout: `order` @0, `grayscale` @4, `opacity` @8, `corner_smoothing`
/// @12, `bounds` @16, `content_mask` @32 (52 bytes, the rev-16 extended
/// mask), `corner_radii` @84, `tile` @100, `pixelated` @132,
/// `record_size` @136; size 140, alignment 4.
///
/// rev 15 (browser-core ticket 39): `pixelated` selects nearest-neighbour
/// sampling for this sprite (CSS `image-rendering: pixelated`); the plan
/// splits sprite batches on it so a batch draws through one sampler.
#[repr(C)]
#[derive(Clone, Copy, PartialEq, Debug)]
pub struct GpuiGoScenePolychromeSpriteRecord {
    /// Draw order. Must be 0 on insert; kernel-assigned in dumps.
    pub order: u32,
    /// 0 = color sampling, 1 = grayscale sampling (pinned `ShaderBool`).
    pub grayscale: u32,
    /// Opacity, f32 bits.
    pub opacity: u32,
    /// Corner smoothing, f32 bits.
    pub corner_smoothing: u32,
    /// Bounds in device pixels.
    pub bounds: GpuiGoSceneBounds,
    /// Content mask bounds.
    /// Content mask (the rev-16 extended mask: bounds, per-edge fade
    /// distances and the rounded-clip radii; a zero-radii, zero-fade
    /// mask behaves exactly as the pre-R16 bounds-only mask).
    pub content_mask: GpuiGoSceneContentMask,
    /// Corner radii, f32 bits, device pixels.
    pub corner_radii: [u32; 4],
    /// The atlas tile this sprite samples.
    pub tile: GpuiGoSceneTileRecord,
    /// 0 = bilinear sampling (the pinned default), 1 = point sampling
    /// (CSS `image-rendering: pixelated`; rev 15).
    pub pixelated: u32,
    /// `size_of::<GpuiGoScenePolychromeSpriteRecord>()` self-check.
    pub record_size: u32,
}

impl Default for GpuiGoScenePolychromeSpriteRecord {
    fn default() -> Self {
        GpuiGoScenePolychromeSpriteRecord {
            order: 0,
            grayscale: 0,
            opacity: 1.0f32.to_bits(),
            corner_smoothing: 0,
            bounds: GpuiGoSceneBounds::default(),
            content_mask: GpuiGoSceneContentMask::default(),
            corner_radii: [0; 4],
            tile: GpuiGoSceneTileRecord::default(),
            pixelated: 0,
            record_size: core::mem::size_of::<GpuiGoScenePolychromeSpriteRecord>() as u32,
        }
    }
}

/// Path command tags for the `path_script` entry (the pinned
/// `PathBuilder` public API surface; see [`GpuiGoPathCommandRecord`]).
pub mod path_command_kind {
    /// `move_to(to)`.
    pub const MOVE_TO: u32 = 0;
    /// `line_to(to)`.
    pub const LINE_TO: u32 = 1;
    /// `curve_to(to, ctrl)` — the pinned quadratic Bézier.
    pub const CURVE_TO: u32 = 2;
    /// `cubic_bezier_to(to, control_a, control_b)`.
    pub const CUBIC_BEZIER_TO: u32 = 3;
    /// `arc_to(radii, x_rotation, large_arc, sweep, to)`.
    pub const ARC_TO: u32 = 4;
    /// `relative_arc_to(...)` — the arc in relative coordinates.
    pub const RELATIVE_ARC_TO: u32 = 5;
    /// `add_polygon(points, closed)` — points indexed into the call's
    /// point pool.
    pub const POLYGON: u32 = 6;
    /// `close()`.
    pub const CLOSE: u32 = 7;
    /// The builder style: fill or stroke options (a setter like the
    /// pinned `with_style`).
    pub const STYLE: u32 = 8;
    /// The stroke dash array (the pinned `dash_array` setter; lengths
    /// indexed into the call's word pool).
    pub const DASH: u32 = 9;
    /// `translate(to)`.
    pub const TRANSLATE: u32 = 10;
    /// `scale(factor)`.
    pub const SCALE: u32 = 11;
    /// `rotate(angle_degrees)`.
    pub const ROTATE: u32 = 12;
    /// `transform(matrix)` — a 2x3 matrix (row-major 2x2 then
    /// translation), replacing the accumulated transform.
    pub const TRANSFORM: u32 = 13;
}

/// Style tags for the [`path_command_kind::STYLE`] command's first data
/// word (the pinned `PathStyle` variants).
pub mod path_style_tag {
    /// `PathStyle::Fill(FillOptions)`.
    pub const FILL: u32 = 0;
    /// `PathStyle::Stroke(StrokeOptions)`.
    pub const STROKE: u32 = 1;
}

/// One path-builder command of a `path_script` call: a kind tag, a
/// 12-word data area (floats as IEEE-754 bits, small enums as tags) and
/// a record-size self-check. Layout: `kind` @0, `data[12]` @4,
/// `record_size` @52; size 56, alignment 4.
///
/// Data areas by kind (word indices):
///
/// * `MOVE_TO`/`LINE_TO`/`TRANSLATE`: `data[0..2]` = x, y (f32 bits);
/// * `CURVE_TO`: `data[0..2]` = `to`, `data[2..4]` = `ctrl` (f32 bits;
///   the pinned signature is `curve_to(to, ctrl)`);
/// * `CUBIC_BEZIER_TO`: `data[0..2]` = `to`, `data[2..4]` = `control_a`,
///   `data[4..6]` = `control_b`;
/// * `ARC_TO`/`RELATIVE_ARC_TO`: `data[0..2]` = radii (x, y),
///   `data[2]` = x rotation in DEGREES (f32 bits), `data[3]` = large_arc
///   (0/1), `data[4]` = sweep (0/1), `data[5..7]` = `to` (x, y);
/// * `POLYGON`: `data[0]` = first point index (the call's point pool,
///   each point two words), `data[1]` = point count, `data[2]` = closed
///   (0/1); the remaining words must be zero;
/// * `CLOSE`: all words zero;
/// * `STYLE` fill: `data[0]` = 0, `data[1]` = tolerance (f32 bits),
///   `data[2]` = fill rule (0 even-odd, 1 non-zero), `data[3]` = sweep
///   orientation (0 vertical, 1 horizontal), `data[4]` = handle
///   intersections (0/1); the remaining words zero;
/// * `STYLE` stroke: `data[0]` = 1, `data[1]` = line width (f32 bits),
///   `data[2]` = start cap (0 butt, 1 square, 2 round), `data[3]` = end
///   cap, `data[4]` = line join (0 miter, 1 round, 2 bevel),
///   `data[5]` = miter limit (f32 bits); the remaining words zero;
/// * `DASH`: `data[0]` = first word index of the dash lengths (the
///   call's word pool), `data[1]` = word count; remaining words zero;
/// * `SCALE`: `data[0]` = factor (f32 bits);
/// * `ROTATE`: `data[0]` = angle in degrees (f32 bits);
/// * `TRANSFORM`: `data[0..6]` = the 2x3 matrix (f32 bits).
#[repr(C)]
#[derive(Clone, Copy, PartialEq, Debug)]
pub struct GpuiGoPathCommandRecord {
    /// A [`path_command_kind`] tag.
    pub kind: u32,
    /// Kind-specific data area (12 words).
    pub data: [u32; 12],
    /// `size_of::<GpuiGoPathCommandRecord>()` self-check.
    pub record_size: u32,
}

impl Default for GpuiGoPathCommandRecord {
    fn default() -> Self {
        GpuiGoPathCommandRecord {
            kind: path_command_kind::MOVE_TO,
            data: [0; 12],
            record_size: core::mem::size_of::<GpuiGoPathCommandRecord>() as u32,
        }
    }
}

/// One tessellated path vertex (the pinned `PathVertex`'s
/// `xy_position` and `st_position`; the pinned per-vertex content mask
/// is always the default — `push_triangle` constructs vertices with
/// `ContentMask::default()` — and is unused by the rasterization path,
/// so the ABI carries only the two positions). Layout: `xy_x` @0,
/// `xy_y` @4, `st_u` @8, `st_v` @12; size 16, alignment 4.
#[repr(C)]
#[derive(Clone, Copy, PartialEq, Debug, Default)]
pub struct GpuiGoScenePathVertexRecord {
    /// Vertex xy position, f32 bits (device pixels in scene records,
    /// logical pixels in `path_script` output).
    pub xy_x: u32,
    /// Vertex y position, f32 bits.
    pub xy_y: u32,
    /// Vertex st texture coordinate u, f32 bits.
    pub st_u: u32,
    /// Vertex st texture coordinate v, f32 bits.
    pub st_v: u32,
}

/// Path primitive record (the pinned `Path<ScaledPixels>` scene
/// primitive: the tessellated triangle list plus the record fields the
/// kernel and renderer consume). Layout: `order` @0, `bounds` @4,
/// `content_mask` @20 (52 bytes, the rev-16 extended mask), `color` @72,
/// `vertex_count` @144, `record_size` @148; size 152, alignment 4.
///
/// rev 15 (browser-core ticket 39): `color` is a full background record
/// (the pinned `Path` holds a `Background`); gradients ride the same
/// CSS-semantics evaluation as quads and shadows.
///
/// The vertices travel as a parallel bulk array (`insert_path` input,
/// `path_dump` output) in `GpuiGoScenePathVertexRecord` form. The
/// pinned `Path::id` (assigned at insertion as the pre-sort array index
/// and read nowhere else) is not carried; the pinned `start`/`current`/
/// `contour_count` are builder-side state and do not survive
/// tessellation.
#[repr(C)]
#[derive(Clone, Copy, PartialEq, Debug)]
pub struct GpuiGoScenePathRecord {
    /// Draw order. Must be 0 on insert; kernel-assigned in dumps.
    pub order: u32,
    /// Bounds (the union of the path's triangles), device pixels.
    pub bounds: GpuiGoSceneBounds,
    /// Content mask bounds.
    /// Content mask (the rev-16 extended mask: bounds, per-edge fade
    /// distances and the rounded-clip radii; a zero-radii, zero-fade
    /// mask behaves exactly as the pre-R16 bounds-only mask).
    pub content_mask: GpuiGoSceneContentMask,
    /// Path fill (solid or a two-stop linear gradient).
    pub color: GpuiGoSceneBackground,
    /// The vertex count of the parallel vertex array.
    pub vertex_count: u32,
    /// `size_of::<GpuiGoScenePathRecord>()` self-check.
    pub record_size: u32,
}

impl Default for GpuiGoScenePathRecord {
    fn default() -> Self {
        GpuiGoScenePathRecord {
            order: 0,
            bounds: GpuiGoSceneBounds::default(),
            content_mask: GpuiGoSceneContentMask::default(),
            color: GpuiGoSceneBackground::default(),
            vertex_count: 0,
            record_size: core::mem::size_of::<GpuiGoScenePathRecord>() as u32,
        }
    }
}

/// One compiled render-plan command. Layout: `command_kind` @0,
/// `primitive_kind` @4, `range_start` @8, `range_end` @12, `smoothed` @16,
/// `pixelated` @20, `texture_index` @24, `rasterization_vertex_count`
/// @28, `sprite_count` @32, `boundary_index` @36,
/// `closing_boundary_index` @40, `filter_target` @44, `target_index`
/// @48, `record_size` @52; size 56, alignment 4.
///
/// `command_kind`: 0 = batch, 1 = begin filter group, 2 = end filter
/// group. `primitive_kind` (batch commands): 0 shadows, 1 quads, 2 paths
/// (reserved), 3 underlines, 4 monochrome sprites (reserved), 5 subpixel
/// sprites (reserved), 6 polychrome sprites (reserved), 7 surfaces,
/// 8 backdrop filters, 9 filter boundary. Filter commands carry the
/// boundary indices and the isolation target (0 inline, 1 isolated;
/// `target_index` is the offscreen target pool index when isolated).
#[repr(C)]
#[derive(Clone, Copy, PartialEq, Debug)]
pub struct GpuiGoSceneCommandRecord {
    pub command_kind: u32,
    pub primitive_kind: u32,
    pub range_start: u32,
    pub range_end: u32,
    /// 0/1, quads and shadows only (corner smoothing split).
    pub smoothed: u32,
    /// 0/1, sprite batches only (rev 15): every sprite in the batch has
    /// `pixelated` set, so the batch draws through the point sampler.
    pub pixelated: u32,
    /// Sprite texture index; 0 when not applicable (reserved kinds).
    pub texture_index: u32,
    /// Path rasterization vertex count; 0 when not applicable.
    pub rasterization_vertex_count: u32,
    /// Path sprite count; 0 when not applicable.
    pub sprite_count: u32,
    /// Filter commands: the opening boundary index. Begin commands repeat
    /// it in `closing_boundary_index`; end commands carry the closing
    /// marker's index there.
    pub boundary_index: u32,
    /// Filter commands: the closing boundary index.
    pub closing_boundary_index: u32,
    /// Filter commands: 0 inline, 1 isolated.
    pub filter_target: u32,
    /// Filter commands: the isolated target pool index (0 when inline).
    pub target_index: u32,
    /// `size_of::<GpuiGoSceneCommandRecord>()` self-check.
    pub record_size: u32,
}

impl Default for GpuiGoSceneCommandRecord {
    fn default() -> Self {
        GpuiGoSceneCommandRecord {
            command_kind: 0,
            primitive_kind: 0,
            range_start: 0,
            range_end: 0,
            smoothed: 0,
            pixelated: 0,
            texture_index: 0,
            rasterization_vertex_count: 0,
            sprite_count: 0,
            boundary_index: 0,
            closing_boundary_index: 0,
            filter_target: 0,
            target_index: 0,
            record_size: core::mem::size_of::<GpuiGoSceneCommandRecord>() as u32,
        }
    }
}

/// Effect-group flag bits of [`GpuiGoSceneEffectRecord::flags`] (rev 16,
/// ticket 50). Bits 8 and up are reserved (zero).
pub mod effect_flags {
    /// 1 = the record opens the group (a start marker); 0 = it closes.
    pub const IS_START: u32 = 1;
    /// The group composites its subtree under the 2x3 matrix (ticket
    /// 51: the subtree renders into the group's LOCAL-frame bucket and
    /// the composite maps it through the record's `transform`).
    pub const HAS_TRANSFORM: u32 = 2;
    /// The group composites its subtree once at `opacity` (group
    /// opacity - the R16 core case).
    pub const HAS_OPACITY: u32 = 4;
    /// The group composites with `blend_mode` instead of source-over
    /// (ticket 52 wires the blend).
    pub const HAS_BLEND: u32 = 8;
    /// The group composites `dst_in` against the mask source (ticket 52
    /// wires the mask).
    pub const HAS_MASK: u32 = 16;
    /// The group is a property node updatable after finish through
    /// `scene_update_property` (ticket 51).
    pub const PROPERTY_NODE: u32 = 32;
    /// The group exists only to clip its subtree to the rounded bounds
    /// (a nested rounded clip - the child clips itself, so the
    /// ancestor's rrect needs an isolated target).
    pub const ROUNDED_CLIP_ONLY: u32 = 64;
    /// The composite clips to the group's rounded `bounds` (masks and
    /// rounded clips; 0 lets content bleed past the bounds, exactly as
    /// content filters do today).
    pub const CLIP_TO_BOUNDS: u32 = 128;
    /// The mask of every known flag bit (validation).
    pub const ALL: u32 = IS_START
        | HAS_TRANSFORM
        | HAS_OPACITY
        | HAS_BLEND
        | HAS_MASK
        | PROPERTY_NODE
        | ROUNDED_CLIP_ONLY
        | CLIP_TO_BOUNDS;
}

/// Effect-group record (rev 16, browser-core ticket 50; primitive
/// class 10). One open/close marker pair carries the whole Chromium
/// effect-node payload: group opacity, the 2x3 transform, blend mode,
/// mask reference, corner radii and the property-node id. Start and
/// end records pair through the SAME boundary machinery as content
/// filter boundaries (start inserts above all prior content, end
/// raises the order floor), and both kinds share one group stack in
/// the compiled plan (command kinds 3/4).
///
/// Ticket 50 wired `has_opacity` (the explicit no-blur `composite_group`
/// path) and the rounded clip (`clip_to_bounds` / `rounded_clip_only`,
/// clipped by the rrect at the composite). Ticket 51 wired the
/// transform (`has_transform`: the subtree renders in the group's
/// LOCAL frame - the records' own coordinates, the group's own
/// bounds/mask/radii with them - and the composite maps the local
/// content region through the row-major 2x3 with linear filtering; a
/// translation-only matrix composites through the axis-aligned path at
/// the translated rect) and the property node
/// (`scene_update_property`, the appendix A.7 in-place update with the
/// old/new mapped bounds). The blend and mask fields are CARRIED and
/// VALIDATED now and wired by ticket 52.
///
/// Layout (x86-64, `#[repr(C)]`): `order` @0, `flags` @4, `bounds` @8,
/// `content_mask` @24 (a plain 16-byte bounds record - the group clip
/// is the rrect from `corner_radii`/`clip_to_bounds`; per-edge fades on
/// a group are expressed as a gradient mask, ticket 52), `transform`
/// @40 (6 words, row-major `[m00, m01, m10, m11, tx, ty]`), `opacity`
/// @64, `blend_mode` @68, `mask_kind` @72, `mask_reference` @76,
/// `mask_mode` @80, `node_id` @84, `corner_radii` @88,
/// `corner_smoothing` @104, `reserved` @108, `record_size` @112;
/// size 116, alignment 4.
#[repr(C)]
#[derive(Clone, Copy, PartialEq, Debug)]
pub struct GpuiGoSceneEffectRecord {
    /// Draw order. Must be 0 on insert; kernel-assigned in dumps.
    pub order: u32,
    /// Effect-group flag bits (see [`effect_flags`]).
    pub flags: u32,
    /// Group bounds in device pixels (the composite region; the
    /// rounded-clip region when `clip_to_bounds` is set).
    pub bounds: GpuiGoSceneBounds,
    /// The group's own rectangular clip (a plain 16-byte bounds
    /// record; the rounded shape comes from `corner_radii` +
    /// `clip_to_bounds`).
    pub content_mask: GpuiGoSceneBounds,
    /// The 2x3 transform, row-major `[m00, m01, m10, m11, tx, ty]`, f32
    /// bits (the sprite matrix shape). Requires an invertible 2x2 when
    /// `has_transform` is set.
    pub transform: [u32; 6],
    /// Group opacity in [0, 1], f32 bits.
    pub opacity: u32,
    /// Blend-mode tag: 0 src-over, 1 multiply, 2 screen, 3 overlay,
    /// 4 darken, 5 lighten, 6 color-dodge, 7 color-burn, 8 hard-light,
    /// 9 soft-light, 10 difference, 11 exclusion, 12 hue, 13 saturation,
    /// 14 color, 15 luminosity, 16 plus-lighter.
    pub blend_mode: u32,
    /// Mask source kind: 0 none, 1 gradient, 2 atlas tile, 3 coverage
    /// (this revision requires 0 - the tables arrive with ticket 52).
    pub mask_kind: u32,
    /// Mask source reference (gradient table index / atlas tile id /
    /// coverage index; 0 when `mask_kind` is 0).
    pub mask_reference: u32,
    /// Mask mode: 0 alpha, 1 luminance.
    pub mask_mode: u32,
    /// Property-node id; 0 = none (requires non-zero when
    /// `property_node` is set).
    pub node_id: u32,
    /// The group's corner radii (top-left, top-right, bottom-right,
    /// bottom-left), f32 bits, device pixels.
    pub corner_radii: [u32; 4],
    /// Corner smoothing in [0, 1], f32 bits.
    pub corner_smoothing: u32,
    /// Must be zero.
    pub reserved: u32,
    /// `size_of::<GpuiGoSceneEffectRecord>()` self-check.
    pub record_size: u32,
}

impl Default for GpuiGoSceneEffectRecord {
    fn default() -> Self {
        GpuiGoSceneEffectRecord {
            order: 0,
            flags: 0,
            bounds: GpuiGoSceneBounds::default(),
            content_mask: GpuiGoSceneBounds::default(),
            transform: [
                1.0f32.to_bits(),
                0.0f32.to_bits(),
                0.0f32.to_bits(),
                1.0f32.to_bits(),
                0.0f32.to_bits(),
                0.0f32.to_bits(),
            ],
            opacity: 1.0f32.to_bits(),
            blend_mode: 0,
            mask_kind: 0,
            mask_reference: 0,
            mask_mode: 0,
            node_id: 0,
            corner_radii: [0; 4],
            corner_smoothing: 0,
            reserved: 0,
            record_size: core::mem::size_of::<GpuiGoSceneEffectRecord>() as u32,
        }
    }
}

/// One gradient stop (rev 16, ticket 52; the design's appendix A.3
/// stop triple): the position along the gradient's parameter and an
/// RGBA8 straight (non-premultiplied) sRGB color packed one byte per
/// lane, `r | g << 8 | b << 16 | a << 24`. The CSS color-stop fixup
/// (ordering, the 0/1 end rules) is resolved by the CALLER before
/// insert; the service interpolates the fixed stops. Layout:
/// `position` @0 (f32 bits), `rgba` @4; size 8, alignment 4.
#[repr(C)]
#[derive(Clone, Copy, PartialEq, Debug)]
pub struct GpuiGoSceneGradientStop {
    /// The stop's position in [0, 1] along the gradient parameter (the
    /// gradient line for linear, the normalized radius for radial, the
    /// sweep fraction for conic), f32 bits.
    pub position: u32,
    /// The stop's straight sRGBA color, one byte per lane.
    pub rgba: u32,
}

impl Default for GpuiGoSceneGradientStop {
    fn default() -> Self {
        GpuiGoSceneGradientStop { position: 0, rgba: 0 }
    }
}

/// Gradient kind tags of [`GpuiGoSceneGradientRecord::kind`] (rev 16,
/// ticket 52).
pub mod gradient_kind {
    /// A linear gradient along the record's `[x0, y0, x1, y1]` line.
    pub const LINEAR: u32 = 0;
    /// A radial gradient (circle or ellipse) around `[cx, cy]`.
    pub const RADIAL: u32 = 1;
    /// A conic gradient sweeping from `start_angle`.
    pub const CONIC: u32 = 2;
}

/// Tile-mode tags of [`GpuiGoSceneGradientRecord::tile`] (rev 16,
/// ticket 52): the wrap rule applied to the parameter BEFORE the ramp
/// is sampled (CSS `repeating-*` sets the repeat tile).
pub mod gradient_tile {
    /// The ramp's own end colors continue beyond [0, 1] (the ramp row
    /// is baked with the first/last stop padded to its ends).
    pub const PAD: u32 = 0;
    /// The parameter wraps with `fract(t)` - CSS `repeating-*`.
    pub const REPEAT: u32 = 1;
    /// The parameter mirrors around the ends (a triangle wave).
    pub const REFLECT: u32 = 2;
}

/// One scene-level gradient table entry (rev 16, ticket 52; the
/// design's appendix A.3): the kind, tile mode, geometry and stop
/// count; the stops travel as the parallel bulk array of
/// `scene_insert_gradient` and are baked into one 1024x1 premultiplied
/// sRGB ramp row the renderer uploads as the ramp texture's row
/// `stop_row`. The endpoint/shape math is resolved by the CALLER
/// (CSS's `EndPointsFromAngle` line, the ellipse's radii, the conic's
/// angles) into the geometry words.
///
/// Layout: `kind` @0, `tile` @4, `color_space` @8 (0 sRGB this
/// revision; 1 Oklab reserved and rejected), `geometry` @12 (8 f32
/// words), `stop_count` @44 (1..=64), `stop_row` @48 (0 on insert; the
/// service assigns the gradient's table index), `record_size` @52;
/// size 56, alignment 4.
#[repr(C)]
#[derive(Clone, Copy, PartialEq, Debug)]
pub struct GpuiGoSceneGradientRecord {
    /// A [`gradient_kind`] tag.
    pub kind: u32,
    /// A [`gradient_tile`] tag.
    pub tile: u32,
    /// 0 sRGB (this revision's only space); 1 Oklab (reserved,
    /// rejected).
    pub color_space: u32,
    /// Kind-specific geometry, f32 bits, device pixels: linear
    /// `[x0, y0, x1, y1]` (the gradient line's endpoints; the rest
    /// zero), radial `[cx, cy, r0, r1, aspect]` (the center, the
    /// start/end radii in ry units, and the horizontal-over-vertical
    /// radius ratio `rx/ry` - 1 for a circle; the rest zero), conic
    /// `[cx, cy, start_angle, rotation]` (the center, the CSS start
    /// angle in RADIANS measured clockwise from up, and the total
    /// sweep in radians - 2*pi for a full turn; the rest zero).
    pub geometry: [u32; 8],
    /// The stop count (1..=64).
    pub stop_count: u32,
    /// The ramp row index (0 on insert; the service assigns the
    /// gradient's own table index, 1:1).
    pub stop_row: u32,
    /// `size_of::<GpuiGoSceneGradientRecord>()` self-check.
    pub record_size: u32,
}

impl Default for GpuiGoSceneGradientRecord {
    fn default() -> Self {
        GpuiGoSceneGradientRecord {
            kind: gradient_kind::LINEAR,
            tile: gradient_tile::PAD,
            color_space: 0,
            geometry: [0; 8],
            stop_count: 0,
            stop_row: 0,
            record_size: core::mem::size_of::<GpuiGoSceneGradientRecord>() as u32,
        }
    }
}

/// One clip-path coverage entry (rev 16, ticket 52; the design's
/// appendix A.4): a reference to one of the scene's tessellated path
/// records plus the path's device bounds. The renderer rasterizes the
/// referenced path into the per-surface A8 coverage target when the
/// frame's effect groups reference the entry (mask kind 3), and the
/// mask composite samples that target. Layout: `path_index` @0,
/// `bounds` @4 (4 words), `record_size` @20; size 24, alignment 4.
#[repr(C)]
#[derive(Clone, Copy, PartialEq, Debug)]
pub struct GpuiGoSceneCoverageRecord {
    /// The referenced path record's index in the scene's path array.
    pub path_index: u32,
    /// The path's device bounds (x, y, w, h, f32 bits).
    pub bounds: GpuiGoSceneBounds,
    /// `size_of::<GpuiGoSceneCoverageRecord>()` self-check.
    pub record_size: u32,
}

impl Default for GpuiGoSceneCoverageRecord {
    fn default() -> Self {
        GpuiGoSceneCoverageRecord {
            path_index: 0,
            bounds: GpuiGoSceneBounds::default(),
            record_size: core::mem::size_of::<GpuiGoSceneCoverageRecord>() as u32,
        }
    }
}

/// Filter-op kinds of [`GpuiGoSceneFilterOpRecord::kind`] (rev 16,
/// ticket 52; the design's appendix A.5).
pub mod filter_op_kind {
    /// A gaussian blur (R15's chain semantics; re-stated for reference
    /// - an op of this kind is NOT insertable, the R15 filter tags carry
    /// the chain's blurs).
    pub const BLUR: u32 = 0;
    /// A colour matrix (R15's chain semantics; re-stated for reference -
    /// not insertable, the R15 matrix slots carry the chain's matrices).
    pub const COLOR_MATRIX: u32 = 1;
    /// A CSS `drop-shadow()`: offset, blur radius and color. The ONLY
    /// insertable kind this revision.
    pub const DROP_SHADOW: u32 = 2;
}

/// One filter-op chain entry (rev 16, ticket 52; the design's appendix
/// A.5): a drop-shadow op - offset (dx, dy), the CSS blur radius (the
/// service stores it as given; the renderer's shadow rule derives
/// sigma = radius / 2, Chromium `shadow_data.h:76-88` BlurAsSigma) and
/// the shadow color. The chain is inserted scene-level
/// (`scene_insert_filter_op_chain`) and referenced APPEND-ONLY from the
/// R15 filter record's `op_chain_index`/`op_chain_count` (0/0 = the R15
/// legacy chain, bit-exact). Layout: `kind` @0, `offset` @4 (2 words),
/// `blur_radius` @12, `color` @16 (a `GpuiGoSceneColor`),
/// `record_size` @36; size 40, alignment 4.
#[repr(C)]
#[derive(Clone, Copy, PartialEq, Debug)]
pub struct GpuiGoSceneFilterOpRecord {
    /// A [`filter_op_kind`] tag (only DROP_SHADOW is insertable).
    pub kind: u32,
    /// The shadow offset dx, dy (device pixels, f32 bits).
    pub offset: [u32; 2],
    /// The CSS blur radius (device pixels, f32 bits; sigma = radius/2
    /// at render).
    pub blur_radius: u32,
    /// The shadow color (a solid `GpuiGoSceneColor`).
    pub color: GpuiGoSceneColor,
    /// `size_of::<GpuiGoSceneFilterOpRecord>()` self-check.
    pub record_size: u32,
}

impl Default for GpuiGoSceneFilterOpRecord {
    fn default() -> Self {
        GpuiGoSceneFilterOpRecord {
            kind: filter_op_kind::DROP_SHADOW,
            offset: [0; 2],
            blur_radius: 0,
            color: GpuiGoSceneColor::default(),
            record_size: core::mem::size_of::<GpuiGoSceneFilterOpRecord>() as u32,
        }
    }
}

/// Scene plan requirements, mirroring the pinned
/// `ScenePlanRequirements` (all counts are `usize` in the pin; the ABI
/// carries u32). Layout: 11 u32 fields; size 44, alignment 4.
#[repr(C)]
#[derive(Clone, Copy, PartialEq, Debug, Default)]
pub struct GpuiGoSceneRequirementsRecord {
    pub command_count: u32,
    pub instance_batch_count: u32,
    pub path_rasterization_vertex_count: u32,
    pub path_sprite_count: u32,
    pub surface_count: u32,
    pub backdrop_filter_count: u32,
    pub isolated_filter_count: u32,
    pub isolated_target_count: u32,
    pub uses_path_target: u32,
    pub uses_offscreen_target: u32,
    pub record_size: u32,
    /// Isolated effect groups composited (rev 16, ticket 50; appended
    /// after `record_size` - the size self-check catches old mirrors).
    /// `isolated_target_count` is the pool high-water mark over BOTH
    /// group kinds.
    pub isolated_effect_count: u32,
}

/// Scene totals for the `scene-meta` observable. Layout: 14 u32 fields;
/// size 56, alignment 4 (ticket10 added the three sprite counts before
/// `is_finished`; ticket16 added `path_count` after the sprite counts;
/// the record-size self-check catches old mirrors).
#[repr(C)]
#[derive(Clone, Copy, PartialEq, Debug, Default)]
pub struct GpuiGoSceneMetaRecord {
    /// Paint-operation count (`Scene::len`).
    pub op_count: u32,
    /// Layer pushes executed (each `push_layer`).
    pub layer_push_count: u32,
    pub quad_count: u32,
    pub shadow_count: u32,
    pub underline_count: u32,
    pub backdrop_count: u32,
    pub boundary_count: u32,
    pub surface_count: u32,
    pub monochrome_sprite_count: u32,
    pub subpixel_sprite_count: u32,
    pub polychrome_sprite_count: u32,
    /// Path primitive count (ticket16).
    pub path_count: u32,
    /// Effect-group marker count (rev 16, ticket 50; appended before
    /// `is_finished` - the size self-check catches old mirrors).
    pub effect_count: u32,
    /// 1 after `scene_finish`.
    pub is_finished: u32,
    pub record_size: u32,
    /// Gradient table entries (rev 16, ticket 52; appended after
    /// `record_size` - the size self-check catches old mirrors).
    pub gradient_count: u32,
    /// Coverage-mask entries (rev 16, ticket 52).
    pub coverage_count: u32,
    /// Filter-op chains (rev 16, ticket 52: the drop-shadow chains the
    /// filter records reference).
    pub filter_chain_count: u32,
}

// Compile-time layout pins; the Go loader mirrors these exactly.
const _: () = assert!(core::mem::size_of::<GpuiGoSceneBounds>() == 16);
const _: () = assert!(core::mem::align_of::<GpuiGoSceneBounds>() == 4);
const _: () = assert!(core::mem::size_of::<GpuiGoSceneColor>() == 20);
const _: () = assert!(core::mem::align_of::<GpuiGoSceneColor>() == 4);
const _: () = assert!(core::mem::size_of::<GpuiGoSceneBackground>() == 72);
const _: () = assert!(core::mem::align_of::<GpuiGoSceneBackground>() == 4);
const _: () = assert!(core::mem::size_of::<GpuiGoSceneQuadRecord>() == 280);
const _: () = assert!(core::mem::align_of::<GpuiGoSceneQuadRecord>() == 4);
const _: () = assert!(core::mem::size_of::<GpuiGoSceneShadowRecord>() == 216);
const _: () = assert!(core::mem::align_of::<GpuiGoSceneShadowRecord>() == 4);
const _: () = assert!(core::mem::size_of::<GpuiGoSceneUnderlineRecord>() == 112);
const _: () = assert!(core::mem::align_of::<GpuiGoSceneUnderlineRecord>() == 4);
const _: () = assert!(core::mem::size_of::<GpuiGoSceneFilterRecord>() == 828);
const _: () = assert!(core::mem::align_of::<GpuiGoSceneFilterRecord>() == 4);
const _: () = assert!(core::mem::size_of::<GpuiGoSceneSurfaceRecord>() == 96);
const _: () = assert!(core::mem::align_of::<GpuiGoSceneSurfaceRecord>() == 4);
const _: () = assert!(core::mem::size_of::<GpuiGoSceneTileRecord>() == 32);
const _: () = assert!(core::mem::align_of::<GpuiGoSceneTileRecord>() == 4);
const _: () = assert!(core::mem::size_of::<GpuiGoSceneSpriteRecord>() == 164);
const _: () = assert!(core::mem::align_of::<GpuiGoSceneSpriteRecord>() == 4);
const _: () = assert!(core::mem::size_of::<GpuiGoScenePolychromeSpriteRecord>() == 144);
const _: () = assert!(core::mem::align_of::<GpuiGoScenePolychromeSpriteRecord>() == 4);
const _: () = assert!(core::mem::size_of::<GpuiGoSceneCommandRecord>() == 56);
const _: () = assert!(core::mem::align_of::<GpuiGoSceneCommandRecord>() == 4);
const _: () = assert!(core::mem::size_of::<GpuiGoSceneRequirementsRecord>() == 48);
const _: () = assert!(core::mem::align_of::<GpuiGoSceneRequirementsRecord>() == 4);
const _: () = assert!(core::mem::size_of::<GpuiGoSceneMetaRecord>() == 72);
const _: () = assert!(core::mem::align_of::<GpuiGoSceneMetaRecord>() == 4);
const _: () = assert!(core::mem::size_of::<GpuiGoScenePathRecord>() == 156);
const _: () = assert!(core::mem::align_of::<GpuiGoScenePathRecord>() == 4);
const _: () = assert!(core::mem::size_of::<GpuiGoScenePathVertexRecord>() == 16);
const _: () = assert!(core::mem::align_of::<GpuiGoScenePathVertexRecord>() == 4);
const _: () = assert!(core::mem::size_of::<GpuiGoPathCommandRecord>() == 56);
const _: () = assert!(core::mem::align_of::<GpuiGoPathCommandRecord>() == 4);
const _: () = assert!(core::mem::size_of::<GpuiGoSceneEffectRecord>() == 116);
const _: () = assert!(core::mem::align_of::<GpuiGoSceneEffectRecord>() == 4);
const _: () = assert!(core::mem::size_of::<GpuiGoSceneContentMask>() == 56);
const _: () = assert!(core::mem::align_of::<GpuiGoSceneContentMask>() == 4);
const _: () = assert!(core::mem::size_of::<GpuiGoSceneGradientRecord>() == 56);
const _: () = assert!(core::mem::align_of::<GpuiGoSceneGradientRecord>() == 4);
const _: () = assert!(core::mem::size_of::<GpuiGoSceneGradientStop>() == 8);
const _: () = assert!(core::mem::align_of::<GpuiGoSceneGradientStop>() == 4);
const _: () = assert!(core::mem::size_of::<GpuiGoSceneCoverageRecord>() == 24);
const _: () = assert!(core::mem::align_of::<GpuiGoSceneCoverageRecord>() == 4);
const _: () = assert!(core::mem::size_of::<GpuiGoSceneFilterOpRecord>() == 40);
const _: () = assert!(core::mem::align_of::<GpuiGoSceneFilterOpRecord>() == 4);
const _: () = assert!(core::mem::size_of::<GpuiGoSceneTable>() == 376);
const _: () = assert!(core::mem::align_of::<GpuiGoSceneTable>() == 8);

// ---------------------------------------------------------------------------
// Handle packing
// ---------------------------------------------------------------------------

/// Scene handle: `(slot index << 20) | slot generation`, slot generation
/// in the low 20 bits (same encoding as the layout engine handles).
const SCENE_HANDLE_SLOT_SHIFT: u32 = 20;
/// Maximum slot generation (20 bits).
const SCENE_GENERATION_MAX: u32 = 0x000F_FFFF;

fn scene_handle_parts(handle: u64) -> (usize, u32) {
    (
        (handle >> SCENE_HANDLE_SLOT_SHIFT) as usize,
        (handle & SCENE_GENERATION_MAX as u64) as u32,
    )
}

fn make_scene_handle(slot: usize, generation: u32) -> u64 {
    ((slot as u64) << SCENE_HANDLE_SLOT_SHIFT) | generation as u64
}

// ---------------------------------------------------------------------------
// Bounds tree (port of crates/gpui/src/bounds_tree.rs, U = f32)
// ---------------------------------------------------------------------------

/// Maximum children per internal node (R-tree style branching factor).
const MAX_CHILDREN: usize = 12;

/// A spatial tree optimized for finding maximum ordering among
/// intersecting bounds; the port of the pinned `BoundsTree<ScaledPixels>`
/// with every structural rule preserved (max-order-child-at-end
/// invariant, fast-path max-leaf check, pruning during search).
#[derive(Debug)]
struct BoundsTree {
    nodes: Vec<TreeNode>,
    root: Option<usize>,
    max_leaf: Option<usize>,
    order_floor: u32,
    insert_path: Vec<usize>,
    search_stack: Vec<usize>,
}

#[derive(Debug, Clone)]
struct TreeNode {
    bounds: F32Bounds,
    max_order: u32,
    kind: TreeNodeKind,
}

#[derive(Debug, Clone)]
enum TreeNodeKind {
    Leaf { order: u32 },
    Internal { children: NodeChildren },
}

/// Fixed-size child-index array keeping the max-order child at the end.
#[derive(Debug, Clone)]
struct NodeChildren {
    indices: [usize; MAX_CHILDREN],
    len: u8,
}

impl NodeChildren {
    fn new() -> Self {
        NodeChildren { indices: [0; MAX_CHILDREN], len: 0 }
    }

    fn push(&mut self, index: usize) {
        debug_assert!((self.len as usize) < MAX_CHILDREN);
        self.indices[self.len as usize] = index;
        self.len += 1;
    }

    fn len(&self) -> usize {
        self.len as usize
    }

    fn as_slice(&self) -> &[usize] {
        &self.indices[..self.len as usize]
    }
}

/// f32 bounds with exactly the pinned `Bounds<f32>` semantics used by the
/// tree: `intersects`, `union`, `half_perimeter` and `dilate`'s
/// arithmetic order.
#[derive(Debug, Clone, Copy, PartialEq, Default)]
struct F32Bounds {
    origin_x: f32,
    origin_y: f32,
    width: f32,
    height: f32,
}

impl F32Bounds {
    fn right(&self) -> f32 {
        self.origin_x + self.width
    }

    fn bottom(&self) -> f32 {
        self.origin_y + self.height
    }

    fn bottom_right(&self) -> (f32, f32) {
        (self.right(), self.bottom())
    }

    fn intersects(&self, other: &F32Bounds) -> bool {
        let (my_x, my_y) = self.bottom_right();
        let (their_x, their_y) = other.bottom_right();
        self.origin_x < their_x
            && my_x > other.origin_x
            && self.origin_y < their_y
            && my_y > other.origin_y
    }

    fn union(&self, other: &F32Bounds) -> F32Bounds {
        let top_left = (
            self.origin_x.min(other.origin_x),
            self.origin_y.min(other.origin_y),
        );
        let (sx, sy) = self.bottom_right();
        let (ox, oy) = other.bottom_right();
        let br = (sx.max(ox), sy.max(oy));
        F32Bounds::from_corners(top_left.0, top_left.1, br.0, br.1)
    }

    fn half_perimeter(&self) -> f32 {
        self.width + self.height
    }

    fn from_corners(tl_x: f32, tl_y: f32, br_x: f32, br_y: f32) -> Self {
        F32Bounds {
            origin_x: tl_x,
            origin_y: tl_y,
            width: br_x - tl_x,
            height: br_y - tl_y,
        }
    }
}

impl Default for BoundsTree {
    fn default() -> Self {
        BoundsTree {
            nodes: Vec::new(),
            root: None,
            max_leaf: None,
            order_floor: 0,
            insert_path: Vec::new(),
            search_stack: Vec::new(),
        }
    }
}

impl BoundsTree {
    fn clear(&mut self) {
        self.nodes.clear();
        self.root = None;
        self.max_leaf = None;
        self.order_floor = 0;
        self.insert_path.clear();
        self.search_stack.clear();
    }

    /// Raise the minimum ordering for subsequent inserts to `floor`
    /// (monotonic).
    fn set_order_floor(&mut self, floor: u32) {
        self.order_floor = self.order_floor.max(floor);
    }

    /// The highest ordering assigned so far (0 if empty).
    fn max_order(&self) -> u32 {
        self.max_leaf.map_or(0, |idx| self.nodes[idx].max_order)
    }

    /// Inserts bounds with an ordering strictly greater than EVERY
    /// existing bounds and returns that ordering.
    fn insert_above_all(&mut self, new_bounds: F32Bounds) -> Result<u32, i32> {
        let ordering = self
            .max_order()
            .checked_add(1)
            .ok_or(scene_status::ERR_BAD_VALUE)?;
        let new_leaf_idx = self.insert_leaf(new_bounds, ordering);
        self.max_leaf = Some(new_leaf_idx);
        Ok(ordering)
    }

    /// Inserts bounds, returning the assigned ordering: one greater than
    /// the maximum ordering of any intersecting bounds, raised to the
    /// order floor.
    fn insert(&mut self, new_bounds: F32Bounds) -> Result<u32, i32> {
        let max_intersecting = self.find_max_ordering(&new_bounds);
        let ordering = (max_intersecting + 1).max(self.order_floor);
        if ordering == 0 {
            // (max_intersecting + 1) overflowed.
            return Err(scene_status::ERR_BAD_VALUE);
        }

        let new_leaf_idx = self.insert_leaf(new_bounds, ordering);
        self.max_leaf = match self.max_leaf {
            None => Some(new_leaf_idx),
            Some(old_idx) if self.nodes[old_idx].max_order < ordering => Some(new_leaf_idx),
            some => some,
        };
        Ok(ordering)
    }

    /// Finds the maximum ordering among all bounds that intersect the
    /// query (fast path: the max-ordering leaf).
    fn find_max_ordering(&mut self, query: &F32Bounds) -> u32 {
        let Some(root_idx) = self.root else {
            return 0;
        };

        if let Some(max_idx) = self.max_leaf {
            let max_node = &self.nodes[max_idx];
            if query.intersects(&max_node.bounds) {
                return max_node.max_order;
            }
        }

        self.search_stack.clear();
        self.search_stack.push(root_idx);

        let mut max_found = 0u32;

        while let Some(node_idx) = self.search_stack.pop() {
            let node = &self.nodes[node_idx];

            if node.max_order <= max_found {
                continue;
            }
            if !query.intersects(&node.bounds) {
                continue;
            }

            match &node.kind {
                TreeNodeKind::Leaf { order } => {
                    max_found = max_found.max(*order);
                }
                TreeNodeKind::Internal { children } => {
                    // Children keep the highest max_order at the end; push
                    // in forward order so the highest is popped first.
                    for &child_idx in children.as_slice().iter() {
                        if self.nodes[child_idx].max_order > max_found {
                            self.search_stack.push(child_idx);
                        }
                    }
                }
            }
        }

        max_found
    }

    /// Inserts a leaf node with the given bounds and ordering, returning
    /// the new leaf's index. Port of the pinned `insert_leaf` including
    /// the R-tree descent (minimum union half-perimeter cost) and the
    /// max-order-child-at-end invariant maintenance.
    fn insert_leaf(&mut self, bounds: F32Bounds, order: u32) -> usize {
        let new_leaf_idx = self.nodes.len();
        self.nodes.push(TreeNode {
            bounds,
            max_order: order,
            kind: TreeNodeKind::Leaf { order },
        });

        let Some(root_idx) = self.root else {
            self.root = Some(new_leaf_idx);
            return new_leaf_idx;
        };

        if matches!(self.nodes[root_idx].kind, TreeNodeKind::Leaf { .. }) {
            let root_bounds = self.nodes[root_idx].bounds;
            let root_order = self.nodes[root_idx].max_order;

            let mut children = NodeChildren::new();
            if order > root_order {
                children.push(root_idx);
                children.push(new_leaf_idx);
            } else {
                children.push(new_leaf_idx);
                children.push(root_idx);
            }

            let new_root_idx = self.nodes.len();
            self.nodes.push(TreeNode {
                bounds: root_bounds.union(&bounds),
                max_order: root_order.max(order),
                kind: TreeNodeKind::Internal { children },
            });
            self.root = Some(new_root_idx);
            return new_leaf_idx;
        }

        self.insert_path.clear();
        let mut current_idx = root_idx;

        loop {
            let (best_child_idx, best_child_pos) = {
                let current = &self.nodes[current_idx];
                let TreeNodeKind::Internal { children } = &current.kind else {
                    unreachable!("should only traverse internal nodes");
                };
                let mut best_child_idx = children.as_slice()[0];
                let mut best_child_pos = 0;
                let mut best_cost = bounds.union(&self.nodes[best_child_idx].bounds).half_perimeter();
                for (pos, &child_idx) in children.as_slice().iter().enumerate().skip(1) {
                    let cost = bounds.union(&self.nodes[child_idx].bounds).half_perimeter();
                    if cost < best_cost {
                        best_cost = cost;
                        best_child_idx = child_idx;
                        best_child_pos = pos;
                    }
                }
                (best_child_idx, best_child_pos)
            };

            self.insert_path.push(current_idx);

            if matches!(self.nodes[best_child_idx].kind, TreeNodeKind::Leaf { .. }) {
                let current_len = {
                    let current = &self.nodes[current_idx];
                    let TreeNodeKind::Internal { children } = &current.kind else {
                        unreachable!()
                    };
                    children.len()
                };
                if current_len < MAX_CHILDREN {
                    let node = &mut self.nodes[current_idx];
                    if let TreeNodeKind::Internal { children } = &mut node.kind {
                        children.push(new_leaf_idx);
                        // The new leaf goes at the end; if it is NOT the new
                        // max, swap it back one so the max stays at the end.
                        if order <= node.max_order {
                            let last = children.len() - 1;
                            children.indices.swap(last - 1, last);
                        }
                    }
                    node.bounds = node.bounds.union(&bounds);
                    node.max_order = node.max_order.max(order);
                    break;
                } else {
                    // Node full: new internal with [best leaf, new leaf].
                    let sibling_bounds = self.nodes[best_child_idx].bounds;
                    let sibling_order = self.nodes[best_child_idx].max_order;

                    let mut new_children = NodeChildren::new();
                    if order > sibling_order {
                        new_children.push(best_child_idx);
                        new_children.push(new_leaf_idx);
                    } else {
                        new_children.push(new_leaf_idx);
                        new_children.push(best_child_idx);
                    }

                    let new_internal_idx = self.nodes.len();
                    let new_internal_max = sibling_order.max(order);
                    self.nodes.push(TreeNode {
                        bounds: sibling_bounds.union(&bounds),
                        max_order: new_internal_max,
                        kind: TreeNodeKind::Internal { children: new_children },
                    });

                    // Replace the leaf with the new internal in the parent.
                    let parent = &mut self.nodes[current_idx];
                    if let TreeNodeKind::Internal { children } = &mut parent.kind {
                        let children_len = children.len();
                        children.indices[best_child_pos] = new_internal_idx;
                        if new_internal_max > parent.max_order {
                            children.indices.swap(best_child_pos, children_len - 1);
                        }
                    }
                    break;
                }
            } else {
                current_idx = best_child_idx;
            }
        }

        // Propagate bounds and max_order updates up the tree, keeping the
        // max-order child at each node's end.
        let mut updated_child_idx: Option<usize> = None;
        for &node_idx in self.insert_path.iter().rev() {
            let node = &mut self.nodes[node_idx];
            node.bounds = node.bounds.union(&bounds);

            if node.max_order < order {
                node.max_order = order;
                if let Some(child_idx) = updated_child_idx {
                    if let TreeNodeKind::Internal { children } = &mut node.kind {
                        if let Some(pos) = children.as_slice().iter().position(|&c| c == child_idx)
                        {
                            let last = children.len() - 1;
                            if pos != last {
                                children.indices.swap(pos, last);
                            }
                        }
                    }
                }
            }
            updated_child_idx = Some(node_idx);
        }

        new_leaf_idx
    }
}

// ---------------------------------------------------------------------------
// Scene state
// ---------------------------------------------------------------------------

/// One path primitive with its bulk vertex array (the replayable form
/// of the pinned `PaintOperation::Primitive(Primitive::Path(path))`).
#[derive(Debug, Clone)]
struct PathData {
    rec: GpuiGoScenePathRecord,
    vertices: Vec<GpuiGoScenePathVertexRecord>,
}

/// One paint operation of the replayable log (the pinned
/// `PaintOperation`).
#[derive(Debug, Clone)]
enum PaintOp {
    Quad(GpuiGoSceneQuadRecord),
    Shadow(GpuiGoSceneShadowRecord),
    Underline(GpuiGoSceneUnderlineRecord),
    Backdrop(GpuiGoSceneFilterRecord),
    Boundary(GpuiGoSceneFilterRecord),
    Surface { surface: GpuiGoSceneSurfaceRecord, opacity: f32 },
    MonochromeSprite(GpuiGoSceneSpriteRecord),
    SubpixelSprite(GpuiGoSceneSpriteRecord),
    PolychromeSprite(GpuiGoScenePolychromeSpriteRecord),
    Path(PathData),
    /// An effect-group marker insert (rev 16, ticket 50).
    Effect(GpuiGoSceneEffectRecord),
    /// A gradient table insert (rev 16, ticket 52).
    Gradient(GradientData),
    /// A coverage-mask entry insert (rev 16, ticket 52).
    Coverage(GpuiGoSceneCoverageRecord),
    /// A filter-op chain insert (rev 16, ticket 52).
    FilterOpChain(Vec<GpuiGoSceneFilterOpRecord>),
    StartLayer(F32Bounds),
    EndLayer,
}

/// One gradient table entry with its stops and the baked ramp row
/// (rev 16, ticket 52): the service bakes the stops into one 1024x1
/// premultiplied sRGB row at insert (the renderer uploads the rows as
/// the ramp texture; the bake is deterministic so the CPU reference in
/// the spec tests mirrors it exactly).
#[derive(Debug, Clone)]
struct GradientData {
    rec: GpuiGoSceneGradientRecord,
    stops: Vec<GpuiGoSceneGradientStop>,
    /// The baked 1024x4-byte premultiplied sRGB ramp row.
    ramp: Vec<u8>,
}

/// The per-scene kernel state (the pinned `Scene`).
#[derive(Debug, Default)]
struct SceneState {
    ops: Vec<PaintOp>,
    bounds_tree: BoundsTree,
    layer_stack: Vec<u32>,
    quads: Vec<GpuiGoSceneQuadRecord>,
    shadows: Vec<GpuiGoSceneShadowRecord>,
    underlines: Vec<GpuiGoSceneUnderlineRecord>,
    backdrops: Vec<GpuiGoSceneFilterRecord>,
    boundaries: Vec<GpuiGoSceneFilterRecord>,
    surfaces: Vec<GpuiGoSceneSurfaceRecord>,
    surface_opacities: Vec<f32>,
    monochrome_sprites: Vec<GpuiGoSceneSpriteRecord>,
    subpixel_sprites: Vec<GpuiGoSceneSpriteRecord>,
    polychrome_sprites: Vec<GpuiGoScenePolychromeSpriteRecord>,
    paths: Vec<PathData>,
    /// Effect-group records (rev 16, ticket 50), sorted by (order,
    /// !is_start) at finish like the filter boundaries.
    effects: Vec<GpuiGoSceneEffectRecord>,
    /// The gradient table (rev 16, ticket 52): insert-order entries
    /// whose `stop_row` is their own index (1:1).
    gradients: Vec<GradientData>,
    /// The coverage-mask entries (rev 16, ticket 52: the clip-path
    /// sources, referencing the path array by index).
    coverage: Vec<GpuiGoSceneCoverageRecord>,
    /// The filter-op chains (rev 16, ticket 52: the drop-shadow chains
    /// the R15 filter records reference by index).
    filter_op_chains: Vec<Vec<GpuiGoSceneFilterOpRecord>>,
    /// How many group START markers (filter boundaries + effect
    /// groups) have been inserted - the `MAX_SCENE_GROUPS` sanity
    /// bound's counter.
    group_starts: u32,
    plan: ScenePlanState,
    layer_push_count: u32,
    is_finished: bool,
    failed: bool,
}

/// The compiled plan (the pinned `ScenePlan`, extended in rev 16 with
/// the single-primitive opacity folds).
#[derive(Debug, Default, Clone)]
struct ScenePlanState {
    commands: Vec<GpuiGoSceneCommandRecord>,
    requirements: GpuiGoSceneRequirementsRecord,
    /// The plan's single-primitive opacity folds (rev 16, ticket 50):
    /// one entry per folded effect group, applied to the SNAPSHOT's
    /// record copies at render time (the stored records stay as
    /// inserted; dumps read them unchanged).
    folds: Vec<SceneFold>,
}

/// Which primitive array a fold multiplies (rev 16).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum SceneFoldPrimitive {
    Quad,
    Shadow,
    Underline,
    Path,
    MonochromeSprite,
    SubpixelSprite,
    PolychromeSprite,
    Surface,
    Backdrop,
}

/// One single-primitive opacity fold: multiply the primitive at
/// `index` of its array by `factor` (rev 16, ticket 50 - the plan's
/// replacement for isolating a one-primitive opacity group).
#[derive(Debug, Clone, Copy, PartialEq)]
struct SceneFold {
    primitive: SceneFoldPrimitive,
    index: u32,
    factor: f32,
}

/// One merged group marker of the plan's shared group stack (rev 16).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
struct GroupMarker {
    kind: GroupMarkerKind,
    index: usize,
    order: u32,
    is_start: bool,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum GroupMarkerKind {
    Filter,
    Effect,
}

/// One entry of the shared render stack (the pinned filter_stack entry,
/// extended with the marker kind).
#[derive(Debug, Clone, Copy)]
struct RenderStackEntry {
    kind: GroupMarkerKind,
    index: usize,
    target: FilterRenderTarget,
}

/// Merges the sorted boundary and effect arrays into the marker stream
/// the BatchIterator emits: (order, PrimitiveKind discriminant) order
/// with boundaries ahead of effects at an exact key tie (the
/// BatchIterator's candidate-list order; an exact cross-kind tie is
/// impossible - the kinds differ).
fn marker_stream(
    boundaries: &[GpuiGoSceneFilterRecord],
    effects: &[GpuiGoSceneEffectRecord],
) -> Vec<GroupMarker> {
    fn kind_of_boundary(boundary: &GpuiGoSceneFilterRecord) -> u32 {
        // FilterBoundaryStart = 0 < EffectGroupStart = 1 < ... <
        // EffectGroupEnd = 11 < FilterBoundaryEnd = 12 in PrimitiveKind.
        if boundary.is_start != 0 {
            0
        } else {
            12
        }
    }
    fn kind_of_effect(effect: &GpuiGoSceneEffectRecord) -> u32 {
        if (effect.flags & effect_flags::IS_START) != 0 {
            1
        } else {
            11
        }
    }
    let mut markers: Vec<GroupMarker> = Vec::with_capacity(boundaries.len() + effects.len());
    let mut boundary_index = 0usize;
    let mut effect_index = 0usize;
    loop {
        let boundary = boundaries.get(boundary_index);
        let effect = effects.get(effect_index);
        let take_boundary = match (boundary, effect) {
            (None, None) => break,
            (Some(_), None) => true,
            (None, Some(_)) => false,
            (Some(boundary), Some(effect)) => {
                let boundary_key = (boundary.order, kind_of_boundary(boundary));
                let effect_key = (effect.order, kind_of_effect(effect));
                boundary_key <= effect_key
            }
        };
        if take_boundary {
            let boundary = boundary.expect("checked");
            markers.push(GroupMarker {
                kind: GroupMarkerKind::Filter,
                index: boundary_index,
                order: boundary.order,
                is_start: boundary.is_start != 0,
            });
            boundary_index += 1;
        } else {
            let effect = effect.expect("checked");
            markers.push(GroupMarker {
                kind: GroupMarkerKind::Effect,
                index: effect_index,
                order: effect.order,
                is_start: (effect.flags & effect_flags::IS_START) != 0,
            });
            effect_index += 1;
        }
    }
    markers
}

/// How many primitives one batch command covers (the fold's span
/// accounting; paths count their records, sprites their range length).
fn batch_primitive_count(batch: &PrimitiveBatch) -> u32 {
    match batch {
        PrimitiveBatch::Shadows { range, .. }
        | PrimitiveBatch::Quads { range, .. }
        | PrimitiveBatch::Underlines(range)
        | PrimitiveBatch::MonochromeSprites { range, .. }
        | PrimitiveBatch::SubpixelSprites { range, .. }
        | PrimitiveBatch::PolychromeSprites { range, .. }
        | PrimitiveBatch::Surfaces(range)
        | PrimitiveBatch::BackdropFilters(range) => range.len() as u32,
        PrimitiveBatch::Paths { range, .. } => range.len() as u32,
        PrimitiveBatch::FilterBoundary(_) | PrimitiveBatch::EffectBoundary(_) => 0,
    }
}

/// The single primitive a one-primitive batch covers, when it is one
/// (the fold's target; None for multi-primitive batches).
fn batch_single_primitive(batch: &PrimitiveBatch) -> Option<SceneFoldPrimitive> {
    let (class, len): (SceneFoldPrimitive, usize) = match batch {
        PrimitiveBatch::Shadows { range, .. } => (SceneFoldPrimitive::Shadow, range.len()),
        PrimitiveBatch::Quads { range, .. } => (SceneFoldPrimitive::Quad, range.len()),
        PrimitiveBatch::Underlines(range) => (SceneFoldPrimitive::Underline, range.len()),
        PrimitiveBatch::MonochromeSprites { range, .. } => {
            (SceneFoldPrimitive::MonochromeSprite, range.len())
        }
        PrimitiveBatch::SubpixelSprites { range, .. } => {
            (SceneFoldPrimitive::SubpixelSprite, range.len())
        }
        PrimitiveBatch::PolychromeSprites { range, .. } => {
            (SceneFoldPrimitive::PolychromeSprite, range.len())
        }
        PrimitiveBatch::Surfaces(range) => (SceneFoldPrimitive::Surface, range.len()),
        PrimitiveBatch::BackdropFilters(range) => (SceneFoldPrimitive::Backdrop, range.len()),
        PrimitiveBatch::Paths { range, .. } => (SceneFoldPrimitive::Path, range.len()),
        PrimitiveBatch::FilterBoundary(_) | PrimitiveBatch::EffectBoundary(_) => {
            return None
        }
    };
    // The fold also needs the primitive's INDEX; the callers read it
    // from the emitted batch command's range. Only singleton batches
    // qualify.
    if len == 1 {
        Some(class)
    } else {
        None
    }
}

impl SceneState {
    fn clear(&mut self) {
        self.ops.clear();
        self.bounds_tree.clear();
        self.layer_stack.clear();
        self.quads.clear();
        self.shadows.clear();
        self.underlines.clear();
        self.backdrops.clear();
        self.boundaries.clear();
        self.surfaces.clear();
        self.surface_opacities.clear();
        self.monochrome_sprites.clear();
        self.subpixel_sprites.clear();
        self.polychrome_sprites.clear();
        self.paths.clear();
        self.effects.clear();
        self.gradients.clear();
        self.coverage.clear();
        self.filter_op_chains.clear();
        self.group_starts = 0;
        self.plan.commands.clear();
        self.plan.requirements = GpuiGoSceneRequirementsRecord::default();
        self.layer_push_count = 0;
        self.is_finished = false;
        self.failed = false;
    }

    /// The clipped bounds of a primitive record: bounds ∩ content mask,
    /// with the pinned `Bounds::intersect` arithmetic (the mask's
    /// rounded-corner fields do not enter the rect intersection: the
    /// innermost rounded clip applies per-pixel in the shader).
    fn clipped_bounds(bounds: &GpuiGoSceneBounds, mask: &GpuiGoSceneBounds) -> F32Bounds {
        let b = bounds.to_f32();
        let m = mask.to_f32();
        let upper_left = (b.origin_x.max(m.origin_x), b.origin_y.max(m.origin_y));
        let (bx, by) = b.bottom_right();
        let (mx, my) = m.bottom_right();
        let br_x = bx.min(mx).max(upper_left.0);
        let br_y = by.min(my).max(upper_left.1);
        F32Bounds::from_corners(upper_left.0, upper_left.1, br_x, br_y)
    }

    fn push_layer(&mut self, bounds: F32Bounds) -> Result<(), i32> {
        if self.ops.len() >= MAX_SCENE_OPERATIONS as usize {
            return Err(scene_status::ERR_LIMIT);
        }
        self.is_finished = false;
        let order = self.bounds_tree.insert(bounds)?;
        self.layer_stack.push(order);
        self.ops.push(PaintOp::StartLayer(bounds));
        self.layer_push_count = self.layer_push_count.saturating_add(1);
        Ok(())
    }

    fn pop_layer(&mut self) -> Result<(), i32> {
        if self.ops.len() >= MAX_SCENE_OPERATIONS as usize {
            return Err(scene_status::ERR_LIMIT);
        }
        self.is_finished = false;
        self.layer_stack.pop();
        self.ops.push(PaintOp::EndLayer);
        Ok(())
    }

    fn raise_order_floor(&mut self) -> Result<(), i32> {
        self.is_finished = false;
        let floor = self
            .bounds_tree
            .max_order()
            .checked_add(1)
            .ok_or(scene_status::ERR_BAD_VALUE)?;
        self.bounds_tree.set_order_floor(floor);
        Ok(())
    }

    /// Whether the primitive at `index` of its class array can carry a
    /// single-primitive opacity FOLD (rev 16, ticket 52): a quad or
    /// shadow whose background references the gradient table (tag 4)
    /// cannot - the fold multiplies the record's stored alpha payload
    /// and the table arm carries none. Every other class folds (the
    /// fold's own multiplication covers their stored colors).
    fn primitive_foldable(&self, class: SceneFoldPrimitive, index: u32) -> bool {
        let background_ok = |bg: &GpuiGoSceneBackground| {
            bg.tag != SCENE_BACKGROUND_TAG_GRADIENT_TABLE
        };
        match class {
            SceneFoldPrimitive::Quad => self
                .quads
                .get(index as usize)
                .map(|quad| background_ok(&quad.background) && background_ok(&quad.border_color))
                .unwrap_or(true),
            SceneFoldPrimitive::Shadow => self
                .shadows
                .get(index as usize)
                .map(|shadow| background_ok(&shadow.color))
                .unwrap_or(true),
            _ => true,
        }
    }

    fn check_insert_capacity(&self, array_len: usize) -> Result<(), i32> {
        if self.ops.len() >= MAX_SCENE_OPERATIONS as usize {
            return Err(scene_status::ERR_LIMIT);
        }
        if array_len >= MAX_SCENE_PRIMITIVES as usize {
            return Err(scene_status::ERR_LIMIT);
        }
        Ok(())
    }

    /// The shared insertion core: computes the clipped bounds, drops
    /// empty non-boundary primitives, selects the order (layer order or a
    /// fresh bounds-tree insert; boundaries insert above all), assigns
    /// the order to the record, appends the primitive array entry and the
    /// paint-op log entry, and raises the floor after a closing boundary.
    /// Returns `None` when the primitive was dropped (no array entry, no
    /// log entry — the pinned behavior).
    fn insert_common(
        &mut self,
        bounds: &GpuiGoSceneBounds,
        mask: &GpuiGoSceneBounds,
        is_group_marker: bool,
    ) -> Result<Option<u32>, i32> {
        let clipped_bounds = Self::clipped_bounds(bounds, mask);
        let clipped_is_empty =
            clipped_bounds.width <= 0.0 || clipped_bounds.height <= 0.0;

        if clipped_is_empty && !is_group_marker {
            return Ok(None);
        }

        let order = if is_group_marker {
            let order_bounds = if clipped_is_empty {
                bounds.to_f32()
            } else {
                clipped_bounds
            };
            self.bounds_tree.insert_above_all(order_bounds)?
        } else {
            match self.layer_stack.last().copied() {
                Some(order) => order,
                None => self.bounds_tree.insert(clipped_bounds)?,
            }
        };
        Ok(Some(order))
    }

    fn insert_quad(&mut self, mut rec: GpuiGoSceneQuadRecord) -> Result<(), i32> {
        self.check_insert_capacity(self.quads.len())?;
        let Some(order) = self.insert_common(&rec.bounds, &rec.content_mask.bounds, false)? else {
            return Ok(());
        };
        self.is_finished = false;
        rec.order = order;
        self.quads.push(rec);
        self.ops.push(PaintOp::Quad(rec));
        Ok(())
    }

    fn insert_shadow(&mut self, mut rec: GpuiGoSceneShadowRecord) -> Result<(), i32> {
        self.check_insert_capacity(self.shadows.len())?;
        let Some(order) = self.insert_common(&rec.bounds, &rec.content_mask.bounds, false)? else {
            return Ok(());
        };
        self.is_finished = false;
        rec.order = order;
        self.shadows.push(rec);
        self.ops.push(PaintOp::Shadow(rec));
        Ok(())
    }

    fn insert_underline(&mut self, mut rec: GpuiGoSceneUnderlineRecord) -> Result<(), i32> {
        self.check_insert_capacity(self.underlines.len())?;
        let Some(order) = self.insert_common(&rec.bounds, &rec.content_mask.bounds, false)? else {
            return Ok(());
        };
        self.is_finished = false;
        rec.order = order;
        self.underlines.push(rec);
        self.ops.push(PaintOp::Underline(rec));
        Ok(())
    }

    fn insert_backdrop(&mut self, mut rec: GpuiGoSceneFilterRecord) -> Result<(), i32> {
        self.check_insert_capacity(self.backdrops.len())?;
        let Some(order) = self.insert_common(&rec.bounds, &rec.content_mask.bounds, false)? else {
            return Ok(());
        };
        self.is_finished = false;
        rec.order = order;
        self.backdrops.push(rec);
        self.ops.push(PaintOp::Backdrop(rec));
        Ok(())
    }

    fn insert_boundary(&mut self, mut rec: GpuiGoSceneFilterRecord) -> Result<(), i32> {
        self.check_insert_capacity(self.boundaries.len())?;
        if rec.is_start != 0 && self.group_starts >= MAX_SCENE_GROUPS {
            return Err(scene_status::ERR_LIMIT);
        }
        // Boundaries are never dropped (matched pairs must survive
        // clipping), so insert_common always yields an order here.
        let order = self
            .insert_common(&rec.bounds, &rec.content_mask.bounds, true)?
            .expect("filter boundaries are never dropped");
        self.is_finished = false;
        rec.order = order;
        if rec.is_start == 0 {
            // A closed content-filter group is a draw-order barrier:
            // everything painted afterwards must sort above the group's
            // end marker (pinned behavior).
            let floor = order
                .checked_add(1)
                .ok_or(scene_status::ERR_BAD_VALUE)?;
            self.bounds_tree.set_order_floor(floor);
        }
        if rec.is_start != 0 {
            self.group_starts = self.group_starts.saturating_add(1);
        }
        self.boundaries.push(rec);
        self.ops.push(PaintOp::Boundary(rec));
        Ok(())
    }

    /// Effect-group marker insertion (rev 16, ticket 50): the SAME
    /// boundary machinery as content-filter boundaries - a start
    /// marker inserts above all prior content (never dropped, even
    /// with an empty clipped rect), a closing marker additionally
    /// raises the order floor above itself so later content cannot
    /// fall back inside the group's order range. Both kinds share one
    /// group stack in the compiled plan.
    fn insert_effect(&mut self, mut rec: GpuiGoSceneEffectRecord) -> Result<(), i32> {
        self.check_insert_capacity(self.effects.len())?;
        if (rec.flags & effect_flags::IS_START) != 0
            && self.group_starts >= MAX_SCENE_GROUPS
        {
            return Err(scene_status::ERR_LIMIT);
        }
        // Boundaries are never dropped (matched pairs must survive
        // clipping), so insert_common always yields an order here.
        let order = self
            .insert_common(&rec.bounds, &rec.content_mask, true)?
            .expect("effect-group markers are never dropped");
        self.is_finished = false;
        rec.order = order;
        if (rec.flags & effect_flags::IS_START) == 0 {
            // A closed group is a draw-order barrier: everything painted
            // afterwards must sort above the group's end marker.
            let floor = order
                .checked_add(1)
                .ok_or(scene_status::ERR_BAD_VALUE)?;
            self.bounds_tree.set_order_floor(floor);
        } else {
            self.group_starts = self.group_starts.saturating_add(1);
        }
        self.effects.push(rec);
        self.ops.push(PaintOp::Effect(rec));
        Ok(())
    }

    /// A gradient table insert (rev 16, ticket 52): validates the
    /// record and stops, bakes the 1024x1 premultiplied-sRGB ramp row
    /// and appends the entry (its `stop_row` is its own table index,
    /// assigned here). The gradient table is NOT ordered - it is a
    /// scene-level table the records reference by index, so no
    /// bounds-tree insert happens; the op log still carries it for
    /// replay (a replayed range re-inserts the table entries it
    /// contains).
    fn insert_gradient(
        &mut self,
        mut rec: GpuiGoSceneGradientRecord,
        stops: &[GpuiGoSceneGradientStop],
    ) -> Result<(), i32> {
        if self.ops.len() >= MAX_SCENE_OPERATIONS as usize {
            return Err(scene_status::ERR_LIMIT);
        }
        if self.gradients.len() >= MAX_SCENE_GRADIENTS as usize {
            return Err(scene_status::ERR_LIMIT);
        }
        let ramp = bake_gradient_ramp(stops);
        rec.stop_row = self.gradients.len() as u32;
        self.is_finished = false;
        let data = GradientData { rec, stops: stops.to_vec(), ramp };
        self.gradients.push(data.clone());
        self.ops.push(PaintOp::Gradient(data));
        Ok(())
    }

    /// A coverage-mask entry insert (rev 16, ticket 52): the clip-path
    /// source referencing one of the scene's path records. The entry is
    /// a scene-level table row (no bounds-tree insert; the op log
    /// carries it for replay).
    fn insert_coverage(&mut self, rec: GpuiGoSceneCoverageRecord) -> Result<(), i32> {
        if self.ops.len() >= MAX_SCENE_OPERATIONS as usize {
            return Err(scene_status::ERR_LIMIT);
        }
        if self.coverage.len() >= MAX_SCENE_COVERAGE as usize {
            return Err(scene_status::ERR_LIMIT);
        }
        self.is_finished = false;
        self.coverage.push(rec);
        self.ops.push(PaintOp::Coverage(rec));
        Ok(())
    }

    /// A filter-op chain insert (rev 16, ticket 52): the drop-shadow
    /// ops the R15 filter records reference by chain index. The chain
    /// index is the insertion order (0-based, returned through the
    /// out parameter).
    fn insert_filter_op_chain(&mut self, ops: &[GpuiGoSceneFilterOpRecord]) -> Result<(), i32> {
        if ops.is_empty() || ops.len() > MAX_SCENE_FILTER_OPS as usize {
            return Err(scene_status::ERR_BAD_VALUE);
        }
        if self.ops.len() >= MAX_SCENE_OPERATIONS as usize {
            return Err(scene_status::ERR_LIMIT);
        }
        if self.filter_op_chains.len() >= MAX_SCENE_FILTER_OP_CHAINS as usize {
            return Err(scene_status::ERR_LIMIT);
        }
        self.is_finished = false;
        self.filter_op_chains.push(ops.to_vec());
        self.ops.push(PaintOp::FilterOpChain(ops.to_vec()));
        Ok(())
    }

    fn insert_surface(
        &mut self,
        mut rec: GpuiGoSceneSurfaceRecord,
        opacity: f32,
    ) -> Result<(), i32> {
        if rec.record_size as usize != core::mem::size_of::<GpuiGoSceneSurfaceRecord>() {
            return Err(scene_status::ERR_BAD_VALUE);
        }
        // Ticket28: the source payload — tag 0 = the pinned
        // `SurfaceSource::Unsupported` stand-in (reserved must be
        // zero); tag 1 = a capture frame handle in reserved[0]
        // (the pinned `SurfaceSource::WindowsCapture` import), the
        // remaining reserved words zero.
        match rec.source_tag {
            surface_source_kind::UNSUPPORTED => {
                if rec.source_reserved != [0, 0, 0] {
                    return Err(scene_status::ERR_BAD_VALUE);
                }
            }
            surface_source_kind::CAPTURE_FRAME => {
                // The 64-bit frame handle packed across the two
                // reserved words (low 32 in [0], high 32 in [1] — the
                // rev-11 fix-up; [1] may be zero for a small handle),
                // the third word zero.
                if rec.source_reserved[0] == 0 || rec.source_reserved[2] != 0 {
                    return Err(scene_status::ERR_BAD_VALUE);
                }
            }
            _ => return Err(scene_status::ERR_BAD_VALUE),
        }
        self.check_insert_capacity(self.surfaces.len())?;
        let Some(order) = self.insert_common(&rec.bounds, &rec.content_mask.bounds, false)? else {
            return Ok(());
        };
        self.is_finished = false;
        rec.order = order;
        self.surfaces.push(rec);
        self.surface_opacities.push(opacity);
        self.ops.push(PaintOp::Surface { surface: rec, opacity });
        Ok(())
    }

    /// Monochrome/subpixel sprite insertion (the pinned
    /// `Scene::insert_primitive(Primitive::MonochromeSprite/SubpixelSprite)`:
    /// same insertion core as every ordinary primitive; empty-clipped
    /// sprites drop).
    fn insert_mask_sprite(
        &mut self,
        mut rec: GpuiGoSceneSpriteRecord,
        class: SpriteClass,
    ) -> Result<(), i32> {
        let array_len = match class {
            SpriteClass::Monochrome => self.monochrome_sprites.len(),
            SpriteClass::Subpixel => self.subpixel_sprites.len(),
            SpriteClass::Polychrome => unreachable!("mask sprites only"),
        };
        self.check_insert_capacity(array_len)?;
        let Some(order) = self.insert_common(&rec.bounds, &rec.content_mask.bounds, false)? else {
            return Ok(());
        };
        self.is_finished = false;
        rec.order = order;
        match class {
            SpriteClass::Monochrome => {
                self.monochrome_sprites.push(rec);
                self.ops.push(PaintOp::MonochromeSprite(rec));
            }
            SpriteClass::Subpixel => {
                self.subpixel_sprites.push(rec);
                self.ops.push(PaintOp::SubpixelSprite(rec));
            }
            SpriteClass::Polychrome => unreachable!("mask sprites only"),
        }
        Ok(())
    }

    /// Polychrome sprite insertion (the pinned
    /// `Scene::insert_primitive(Primitive::PolychromeSprite)`).
    fn insert_polychrome_sprite(
        &mut self,
        mut rec: GpuiGoScenePolychromeSpriteRecord,
    ) -> Result<(), i32> {
        self.check_insert_capacity(self.polychrome_sprites.len())?;
        let Some(order) = self.insert_common(&rec.bounds, &rec.content_mask.bounds, false)? else {
            return Ok(());
        };
        self.is_finished = false;
        rec.order = order;
        self.polychrome_sprites.push(rec);
        self.ops.push(PaintOp::PolychromeSprite(rec));
        Ok(())
    }

    /// Path insertion (the pinned `Scene::insert_primitive(Primitive::Path(path))`:
    /// same insertion core as every ordinary primitive; empty-clipped
    /// paths drop, and the vertex array is copied into scene storage).
    fn insert_path(
        &mut self,
        mut rec: GpuiGoScenePathRecord,
        vertices: Vec<GpuiGoScenePathVertexRecord>,
    ) -> Result<(), i32> {
        self.check_insert_capacity(self.paths.len())?;
        let Some(order) = self.insert_common(&rec.bounds, &rec.content_mask.bounds, false)? else {
            // The pinned behavior: an empty-clipped path disappears (no
            // array entry, no log entry).
            return Ok(());
        };
        self.is_finished = false;
        rec.order = order;
        let data = PathData { rec, vertices };
        self.paths.push(data.clone());
        self.ops.push(PaintOp::Path(data));
        Ok(())
    }

    fn replay(&mut self, start: usize, end: usize, prev_ops: Vec<PaintOp>) -> Result<(), i32> {
        for op in prev_ops {
            match op {
                PaintOp::Quad(rec) => self.insert_quad(rec)?,
                PaintOp::Shadow(rec) => self.insert_shadow(rec)?,
                PaintOp::Underline(rec) => self.insert_underline(rec)?,
                PaintOp::Backdrop(rec) => self.insert_backdrop(rec)?,
                PaintOp::Boundary(rec) => self.insert_boundary(rec)?,
                PaintOp::Surface { surface, opacity } => self.insert_surface(surface, opacity)?,
                PaintOp::MonochromeSprite(rec) => {
                    self.insert_mask_sprite(rec, SpriteClass::Monochrome)?
                }
                PaintOp::SubpixelSprite(rec) => {
                    self.insert_mask_sprite(rec, SpriteClass::Subpixel)?
                }
                PaintOp::PolychromeSprite(rec) => self.insert_polychrome_sprite(rec)?,
                PaintOp::Path(data) => self.insert_path(data.rec, data.vertices)?,
                PaintOp::Effect(rec) => self.insert_effect(rec)?,
                PaintOp::Gradient(data) => {
                    self.insert_gradient(data.rec, &data.stops)?;
                }
                PaintOp::Coverage(rec) => self.insert_coverage(rec)?,
                PaintOp::FilterOpChain(ops) => self.insert_filter_op_chain(&ops)?,
                PaintOp::StartLayer(bounds) => self.push_layer(bounds)?,
                PaintOp::EndLayer => self.pop_layer()?,
            }
        }
        let _ = (start, end);
        Ok(())
    }

    fn finish(&mut self) -> Result<(), i32> {
        self.quads.sort_by_key(|rec| rec.order);
        self.shadows.sort_by_key(|rec| rec.order);
        self.underlines.sort_by_key(|rec| rec.order);
        // Surfaces and their opacities sort together as pairs (the pinned
        // zip/sort/unzip), preserving the pairing.
        let surfaces = std::mem::take(&mut self.surfaces);
        let mut surface_opacities = std::mem::take(&mut self.surface_opacities);
        surface_opacities.resize(surfaces.len(), 1.0);
        surface_opacities.truncate(surfaces.len());
        let mut surfaces_with_opacity: Vec<(GpuiGoSceneSurfaceRecord, f32)> = surfaces
            .into_iter()
            .zip(surface_opacities)
            .collect();
        surfaces_with_opacity.sort_by_key(|(surface, _)| surface.order);
        let (surfaces, surface_opacities): (Vec<_>, Vec<_>) =
            surfaces_with_opacity.into_iter().unzip();
        self.surfaces = surfaces;
        self.surface_opacities = surface_opacities;
        self.backdrops.sort_by_key(|rec| rec.order);
        // Sprites sort by (order, tile id): the pinned tie-break
        // (`scene.rs:258-263`).
        self.monochrome_sprites.sort_by_key(|rec| (rec.order, rec.tile.tile_id));
        self.subpixel_sprites.sort_by_key(|rec| (rec.order, rec.tile.tile_id));
        self.polychrome_sprites.sort_by_key(|rec| (rec.order, rec.tile.tile_id));
        // Paths stable-sort by draw order (the pinned
        // `self.paths.sort_by_key(|path| path.order)`).
        self.paths.sort_by_key(|data| data.rec.order);
        // Effect markers follow the same rule (start ahead of end at an
        // equal order; rev 16, ticket 50).
        self.effects.sort_by_key(|rec| (rec.order, rec.flags & effect_flags::IS_START == 0));
        // Markers normally get distinct, monotonically-increasing orders;
        // the `!is_start` tiebreak only matters for a degenerate empty
        // group whose start and end tie: it keeps the start ahead of the
        // end so the pair stays well-formed.
        self.boundaries.sort_by_key(|rec| (rec.order, rec.is_start == 0));
        self.build_plan()?;
        self.is_finished = true;
        Ok(())
    }

    fn build_plan(&mut self) -> Result<(), i32> {
        let mut commands: Vec<GpuiGoSceneCommandRecord> =
            std::mem::take(&mut self.plan.commands);
        commands.clear();
        let mut folds: Vec<SceneFold> = std::mem::take(&mut self.plan.folds);
        folds.clear();
        let mut requirements = GpuiGoSceneRequirementsRecord {
            record_size: core::mem::size_of::<GpuiGoSceneRequirementsRecord>() as u32,
            ..Default::default()
        };

        // The merged marker stream: boundary and effect markers in the
        // same (order, kind) order the BatchIterator emits them (rev 16,
        // ticket 50: both kinds share ONE group stack).
        let markers = marker_stream(&self.boundaries, &self.effects);

        // Matching pre-pass (the pinned pending_starts, extended to two
        // kinds): an end marker pops the most recent unmatched start and
        // matches ONLY when the kinds agree; a mismatched end restores
        // the stack top and stays unmatched (it renders inline, like the
        // pin's unmatched ends). Malformed interleaved markers are
        // degenerate by design - well-nested groups composite correctly.
        let mut matched: Vec<bool> = vec![false; markers.len()];
        let mut pending: Vec<usize> = Vec::new();
        for (position, marker) in markers.iter().enumerate() {
            if marker.is_start {
                pending.push(position);
            } else if let Some(start_position) = pending.pop() {
                if markers[start_position].kind == marker.kind {
                    matched[start_position] = true;
                    matched[position] = true;
                } else {
                    pending.push(start_position);
                }
            }
        }

        // The shared render stack (the pinned filter_stack, extended):
        // every START pushes (matched or not - exactly the pin's
        // count-balanced walk) and every END pops the top. The pool
        // target slot is the number of currently open ISOLATED groups -
        // a depth-based assignment, so sibling groups reuse one slot and
        // scenes nested at most two deep produce EXACTLY the R15 plan
        // (target indices 0 and 1). There is no depth cap (rev 16): the
        // renderer's per-surface pool budget is the only demotion point.
        let mut render_stack: Vec<RenderStackEntry> = Vec::new();
        let mut open_isolated: usize = 0;

        // Per open group, the span accounting for the single-primitive
        // opacity fold: how many primitives and nested markers the span
        // contains, the span's single primitive, and the begin command's
        // position (patched back to inline when the fold applies).
        let mut span_primitives: Vec<u32> = Vec::new();
        let mut span_markers: Vec<u32> = Vec::new();
        let mut span_single: Vec<Option<(SceneFoldPrimitive, u32)>> = Vec::new();
        let mut span_begin_command: Vec<usize> = Vec::new();

        for batch in BatchIterator::new(self) {
            let is_marker = matches!(
                batch,
                PrimitiveBatch::FilterBoundary(_) | PrimitiveBatch::EffectBoundary(_)
            );
            // A nested marker disqualifies every OUTER group's fold.
            if is_marker && span_markers.len() > 1 {
                if let Some(outer) = span_markers.iter_mut().rev().nth(1) {
                    *outer = outer.saturating_add(1);
                }
            }
            match batch {
                PrimitiveBatch::FilterBoundary(boundary_index) => {
                    let boundary = &self.boundaries[boundary_index];
                    let position = markers
                        .iter()
                        .position(|m| m.kind == GroupMarkerKind::Filter && m.index == boundary_index)
                        .expect("boundary marker is in the merged stream");
                    if boundary.is_start != 0 {
                        let target = if matched[position] {
                            FilterRenderTarget::Isolated(open_isolated)
                        } else {
                            FilterRenderTarget::Inline
                        };
                        if target.is_isolated() {
                            open_isolated += 1;
                            requirements.uses_offscreen_target = 1;
                            requirements.isolated_target_count =
                                requirements.isolated_target_count.max(open_isolated as u32);
                        }
                        render_stack.push(RenderStackEntry {
                            kind: GroupMarkerKind::Filter,
                            index: boundary_index,
                            target,
                        });
                        span_primitives.push(0);
                        span_markers.push(0);
                        span_single.push(None);
                        span_begin_command.push(commands.len());
                        commands.push(command_record(
                            command_kind::BEGIN_FILTER,
                            boundary_index,
                            boundary_index,
                            target,
                        ));
                    } else {
                        let entry = render_stack
                            .pop()
                            .filter(|entry| entry.kind == GroupMarkerKind::Filter);
                        let (start_index, target) = match entry {
                            Some(entry) => (entry.index, entry.target),
                            None => {
                                // An end with no boundary start on the
                                // stack top: inline (the pinned unmatched
                                // end - an empty stack emits the marker's
                                // own index; a kind mismatch keeps the
                                // deeper group open; malformed interleaved
                                // markers stay degenerate, never
                                // misread).
                                (boundary_index, FilterRenderTarget::Inline)
                            }
                        };
                        if target.is_isolated() {
                            open_isolated -= 1;
                            requirements.isolated_filter_count += 1;
                        }
                        span_primitives.pop();
                        span_markers.pop();
                        span_single.pop();
                        span_begin_command.pop();
                        commands.push(command_record(
                            command_kind::END_FILTER,
                            start_index,
                            boundary_index,
                            target,
                        ));
                    }
                }
                PrimitiveBatch::EffectBoundary(effect_index) => {
                    let effect = &self.effects[effect_index];
                    let position = markers
                        .iter()
                        .position(|m| m.kind == GroupMarkerKind::Effect && m.index == effect_index)
                        .expect("effect marker is in the merged stream");
                    let is_start = (effect.flags & effect_flags::IS_START) != 0;
                    if is_start {
                        // rev 16, ticket 51: a matched effect group with
                        // a transform ALWAYS isolates - the translation
                        // included (the composite is a lossless blit at
                        // the translated rect for integral offsets).
                        // The design §3.4 "translation-only skips the
                        // group" economy is a GO-side paint decision
                        // (the element offset the paint context already
                        // applies - the records carry the translated
                        // positions and no group is inserted), not a
                        // native walk mechanism: rendering the children
                        // at an offset through the viewport would break
                        // the shaders' fragment invariant (SV_Position
                        // must equal the records' coordinate space),
                        // the same root cause as 50's handed-off
                        // sprite-bucket gap.
                        let target = if matched[position] {
                            FilterRenderTarget::Isolated(open_isolated)
                        } else {
                            FilterRenderTarget::Inline
                        };
                        if target.is_isolated() {
                            open_isolated += 1;
                            requirements.isolated_target_count =
                                requirements.isolated_target_count.max(open_isolated as u32);
                            // rev 16, ticket 52: a blend group forces the
                            // OFFSCREEN route - the composite blends
                            // against a backdrop COPY sampled from the
                            // parent target, so the base target must be a
                            // readable texture (the offscreen scene
                            // texture, not the swapchain back buffer).
                            if (effect.flags & effect_flags::HAS_BLEND) != 0 {
                                requirements.uses_offscreen_target = 1;
                            }
                        }
                        render_stack.push(RenderStackEntry {
                            kind: GroupMarkerKind::Effect,
                            index: effect_index,
                            target,
                        });
                        span_primitives.push(0);
                        span_markers.push(0);
                        span_single.push(None);
                        span_begin_command.push(commands.len());
                        commands.push(command_record(
                            command_kind::BEGIN_EFFECT,
                            effect_index,
                            effect_index,
                            target,
                        ));
                    } else {
                        let entry = render_stack
                            .pop()
                            .filter(|entry| entry.kind == GroupMarkerKind::Effect);
                        match entry {
                            Some(entry) => {
                                let mut target = entry.target;
                                // The group's parameters are the START
                                // marker's record (the pair's shared
                                // snapshot, the pinned with_filter_layer
                                // rule; the END marker only closes).
                                let start_effect = &self.effects[entry.index];
                                // The single-primitive opacity fold
                                // (Chromium's "at least two layers draw
                                // content" surface rule,
                                // property_tree_builder.cc:395-403): a
                                // pure-opacity effect group whose span
                                // holds exactly ONE primitive and no
                                // nested group renders INLINE - the plan
                                // emits inline begin/end commands and the
                                // group's opacity multiplies that one
                                // primitive's colour (the snapshot applies
                                // the fold; today's per-primitive
                                // behaviour, which is correct for one
                                // primitive).
                                let foldable = (start_effect.flags
                                    & (effect_flags::HAS_TRANSFORM
                                        | effect_flags::HAS_BLEND
                                        | effect_flags::HAS_MASK
                                        | effect_flags::PROPERTY_NODE
                                        | effect_flags::ROUNDED_CLIP_ONLY
                                        | effect_flags::CLIP_TO_BOUNDS))
                                    == 0
                                    && span_markers.last().copied() == Some(0)
                                    && span_primitives.last().copied() == Some(1)
                                    && span_single.last().is_some_and(|single| single.is_some())
                                    && span_single
                                        .last()
                                        .and_then(|single| *single)
                                        .is_some_and(|(class, index)| {
                                            // rev 16, ticket 52: a quad or shadow
                                            // referencing the GRADIENT TABLE
                                            // (background tag 4) cannot fold - the
                                            // fold multiplies the record's stored
                                            // alpha payload, and the table arm
                                            // carries none (the ramp is shared by
                                            // every referencing record). Such a
                                            // group isolates instead (always
                                            // correct, never wrong pixels).
                                            self.primitive_foldable(class, index)
                                        });
                                if foldable {
                                    let primitive = span_single.last().copied().flatten();
                                    let begin_command = span_begin_command.last().copied();
                                    if let (Some(primitive), Some(begin_command)) =
                                        (primitive, begin_command)
                                    {
                                        let factor = f32::from_bits(start_effect.opacity);
                                        folds.push(SceneFold {
                                            primitive: primitive.0,
                                            index: primitive.1,
                                            factor,
                                        });
                                        if target.is_isolated() {
                                            open_isolated -= 1;
                                        }
                                        // Patch the begin command back to
                                        // inline (the group never opens a
                                        // target).
                                        target = FilterRenderTarget::Inline;
                                        if let Some(begin) = commands.get_mut(begin_command) {
                                            begin.filter_target = 0;
                                            begin.target_index = 0;
                                        }
                                    }
                                }
                                if target.is_isolated() {
                                    open_isolated -= 1;
                                    requirements.isolated_effect_count += 1;
                                }
                                commands.push(command_record(
                                    command_kind::END_EFFECT,
                                    entry.index,
                                    effect_index,
                                    target,
                                ));
                            }
                            None => {
                                commands.push(command_record(
                                    command_kind::END_EFFECT,
                                    effect_index,
                                    effect_index,
                                    FilterRenderTarget::Inline,
                                ));
                            }
                        }
                        span_primitives.pop();
                        span_markers.pop();
                        span_single.pop();
                        span_begin_command.pop();
                    }
                }
                batch => {
                    // Span accounting for the fold: the innermost open
                    // group's primitive count and single-primitive
                    // candidate (class + array index).
                    if let Some(last) = span_primitives.last_mut() {
                        *last = last.saturating_add(batch_primitive_count(&batch));
                    }
                    let record = batch_command_record(&batch);
                    let single =
                        batch_single_primitive(&batch).map(|class| (class, record.range_start));
                    if let Some(entry) = span_single.last_mut() {
                        match (*entry, single) {
                            (None, Some(single)) => *entry = Some(single),
                            (Some(_), Some(_)) => *entry = None,
                            _ => {}
                        }
                    }
                    include_batch(&mut requirements, &batch);
                    commands.push(record);
                }
            }
            if commands.len() > MAX_SCENE_PLAN_COMMANDS as usize {
                return Err(scene_status::ERR_LIMIT);
            }
        }

        requirements.command_count = commands.len() as u32;
        self.plan.commands = commands;
        self.plan.folds = folds;
        self.plan.requirements = requirements;
        Ok(())
    }
}

impl GpuiGoSceneBounds {
    fn to_f32(&self) -> F32Bounds {
        F32Bounds {
            origin_x: f32::from_bits(self.x),
            origin_y: f32::from_bits(self.y),
            width: f32::from_bits(self.w),
            height: f32::from_bits(self.h),
        }
    }

    #[cfg(test)] // only the tests use the f32->record direction so far
    fn from_f32(bounds: F32Bounds) -> Self {
        GpuiGoSceneBounds {
            x: bounds.origin_x.to_bits(),
            y: bounds.origin_y.to_bits(),
            w: bounds.width.to_bits(),
            h: bounds.height.to_bits(),
        }
    }
}

// ---------------------------------------------------------------------------
// Batches and the plan (port of scene.rs BatchIterator + plan.rs)
// ---------------------------------------------------------------------------

/// Primitive kinds with the pinned `PrimitiveKind` discriminant ORDER
/// (the tie-break when kinds share an order). All kinds are emitted
/// since ticket16 (scene service v3 added paths to the batch stream).
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
enum PrimitiveKind {
    /// Lowest discriminant: a group-start is emitted before the group's
    /// own content at an equal order.
    FilterBoundaryStart,
    /// Effect-group start (rev 16): sorts with the filter starts - both
    /// kinds open a group before its content at an equal order. The
    /// relative order of the PRE-EXISTING kinds is unchanged, so no
    /// pre-R16 scene can reorder.
    EffectGroupStart,
    Shadow,
    Quad,
    Path,
    Underline,
    MonochromeSprite,
    SubpixelSprite,
    PolychromeSprite,
    Surface,
    BackdropFilter,
    /// Effect-group end (rev 16): sorts just under the filter ends.
    EffectGroupEnd,
    /// Highest discriminant: a group-end is emitted after the group's
    /// content at an equal order.
    FilterBoundaryEnd,
}

/// One merged run of one primitive kind (the pinned `PrimitiveBatch`,
/// restricted to the supported kinds).
#[derive(Debug, Clone, PartialEq)]
enum PrimitiveBatch {
    Shadows { range: std::ops::Range<usize>, smoothed: bool },
    Quads { range: std::ops::Range<usize>, smoothed: bool },
    Paths { range: std::ops::Range<usize>, rasterization_vertex_count: usize, sprite_count: usize },
    Underlines(std::ops::Range<usize>),
    MonochromeSprites { texture_index: u32, range: std::ops::Range<usize>, pixelated: bool },
    SubpixelSprites { texture_index: u32, range: std::ops::Range<usize>, pixelated: bool },
    PolychromeSprites { texture_index: u32, range: std::ops::Range<usize>, smoothed: bool, pixelated: bool },
    Surfaces(std::ops::Range<usize>),
    BackdropFilters(std::ops::Range<usize>),
    FilterBoundary(usize),
    /// One effect-group marker (rev 16, ticket 50), emitted one at a
    /// time like the filter boundaries.
    EffectBoundary(usize),
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum FilterRenderTarget {
    Inline,
    Isolated(usize),
}

impl FilterRenderTarget {
    fn is_isolated(&self) -> bool {
        matches!(self, Self::Isolated(_))
    }
}

/// Command-kind tags of the command record.
pub(crate) mod command_kind {
    pub const BATCH: u32 = 0;
    pub const BEGIN_FILTER: u32 = 1;
    pub const END_FILTER: u32 = 2;
    /// Begin effect group (rev 16, ticket 50): the command carries the
    /// effect record index in `boundary_index` and the pool target slot
    /// in `target_index` (`filter_target` 1 = isolated).
    pub const BEGIN_EFFECT: u32 = 3;
    /// End effect group: `boundary_index` is the opening record's index,
    /// `closing_boundary_index` the closing marker's.
    pub const END_EFFECT: u32 = 4;
}

/// Batch primitive-kind tags of the command record. The sprite tags are
/// emitted since ticket10; the path tag since ticket16 (scene service
/// v3).
pub(crate) mod batch_primitive_kind {
    pub const SHADOWS: u32 = 0;
    pub const QUADS: u32 = 1;
    pub const PATHS: u32 = 2;
    pub const UNDERLINES: u32 = 3;
    pub const MONOCHROME_SPRITES: u32 = 4;
    pub const SUBPIXEL_SPRITES: u32 = 5;
    pub const POLYCHROME_SPRITES: u32 = 6;
    pub const SURFACES: u32 = 7;
    pub const BACKDROP_FILTERS: u32 = 8;
    pub const FILTER_BOUNDARY: u32 = 9;
    /// Effect-group marker (rev 16, ticket 50; never a BATCH command -
    /// markers compile to begin/end effect commands).
    pub const EFFECT_BOUNDARY: u32 = 10;
}

fn has_corner_smoothing(corner_smoothing: u32) -> bool {
    f32::from_bits(corner_smoothing) > 0.0
}

fn precedes_limit(
    order: u32,
    kind: PrimitiveKind,
    limit: Option<(u32, PrimitiveKind)>,
) -> bool {
    limit.is_none_or(|limit| (order, kind) < limit)
}

fn command_record(
    kind: u32,
    boundary_index: usize,
    closing_boundary_index: usize,
    target: FilterRenderTarget,
) -> GpuiGoSceneCommandRecord {
    let (filter_target, target_index) = match target {
        FilterRenderTarget::Inline => (0u32, 0u32),
        FilterRenderTarget::Isolated(index) => (1u32, index as u32),
    };
    GpuiGoSceneCommandRecord {
        command_kind: kind,
        primitive_kind: batch_primitive_kind::FILTER_BOUNDARY,
        range_start: 0,
        range_end: 0,
        smoothed: 0,
        pixelated: 0,
        texture_index: 0,
        rasterization_vertex_count: 0,
        sprite_count: 0,
        boundary_index: boundary_index as u32,
        closing_boundary_index: closing_boundary_index as u32,
        filter_target,
        target_index,
        record_size: core::mem::size_of::<GpuiGoSceneCommandRecord>() as u32,
    }
}

fn batch_command_record(batch: &PrimitiveBatch) -> GpuiGoSceneCommandRecord {
    let mut rec = GpuiGoSceneCommandRecord {
        command_kind: command_kind::BATCH,
        record_size: core::mem::size_of::<GpuiGoSceneCommandRecord>() as u32,
        ..Default::default()
    };
    match batch {
        PrimitiveBatch::Shadows { range, smoothed } => {
            rec.primitive_kind = batch_primitive_kind::SHADOWS;
            rec.range_start = range.start as u32;
            rec.range_end = range.end as u32;
            rec.smoothed = u32::from(*smoothed);
        }
        PrimitiveBatch::Quads { range, smoothed } => {
            rec.primitive_kind = batch_primitive_kind::QUADS;
            rec.range_start = range.start as u32;
            rec.range_end = range.end as u32;
            rec.smoothed = u32::from(*smoothed);
        }
        PrimitiveBatch::Paths { range, rasterization_vertex_count, sprite_count } => {
            rec.primitive_kind = batch_primitive_kind::PATHS;
            rec.range_start = range.start as u32;
            rec.range_end = range.end as u32;
            rec.rasterization_vertex_count = *rasterization_vertex_count as u32;
            rec.sprite_count = *sprite_count as u32;
        }
        PrimitiveBatch::Underlines(range) => {
            rec.primitive_kind = batch_primitive_kind::UNDERLINES;
            rec.range_start = range.start as u32;
            rec.range_end = range.end as u32;
        }
        PrimitiveBatch::MonochromeSprites { texture_index, range, pixelated } => {
            rec.primitive_kind = batch_primitive_kind::MONOCHROME_SPRITES;
            rec.range_start = range.start as u32;
            rec.range_end = range.end as u32;
            rec.pixelated = u32::from(*pixelated);
            // The batch's atlas texture (the pinned
            // `PrimitiveBatch::MonochromeSprites { texture_id, range }`;
            // the kind pool is implied by the primitive kind).
            rec.texture_index = *texture_index;
        }
        PrimitiveBatch::SubpixelSprites { texture_index, range, pixelated } => {
            rec.primitive_kind = batch_primitive_kind::SUBPIXEL_SPRITES;
            rec.range_start = range.start as u32;
            rec.range_end = range.end as u32;
            rec.pixelated = u32::from(*pixelated);
            rec.texture_index = *texture_index;
        }
        PrimitiveBatch::PolychromeSprites { texture_index, range, smoothed, pixelated } => {
            rec.primitive_kind = batch_primitive_kind::POLYCHROME_SPRITES;
            rec.range_start = range.start as u32;
            rec.range_end = range.end as u32;
            rec.texture_index = *texture_index;
            rec.smoothed = u32::from(*smoothed);
            rec.pixelated = u32::from(*pixelated);
        }
        PrimitiveBatch::Surfaces(range) => {
            rec.primitive_kind = batch_primitive_kind::SURFACES;
            rec.range_start = range.start as u32;
            rec.range_end = range.end as u32;
        }
        PrimitiveBatch::BackdropFilters(range) => {
            rec.primitive_kind = batch_primitive_kind::BACKDROP_FILTERS;
            rec.range_start = range.start as u32;
            rec.range_end = range.end as u32;
        }
        PrimitiveBatch::FilterBoundary(index) => {
            rec.primitive_kind = batch_primitive_kind::FILTER_BOUNDARY;
            rec.range_start = *index as u32;
            rec.range_end = *index as u32;
        }
        PrimitiveBatch::EffectBoundary(index) => {
            rec.primitive_kind = batch_primitive_kind::EFFECT_BOUNDARY;
            rec.range_start = *index as u32;
            rec.range_end = *index as u32;
        }
    }
    rec
}

/// Requirements collection (the pinned
/// `ScenePlanRequirements::include_batch`).
fn include_batch(requirements: &mut GpuiGoSceneRequirementsRecord, batch: &PrimitiveBatch) {
    let non_empty = |range: &std::ops::Range<usize>| u32::from(!range.is_empty());
    match batch {
        PrimitiveBatch::Shadows { range, .. }
        | PrimitiveBatch::Quads { range, .. }
        | PrimitiveBatch::Underlines(range)
        | PrimitiveBatch::MonochromeSprites { range, .. }
        | PrimitiveBatch::SubpixelSprites { range, .. }
        | PrimitiveBatch::PolychromeSprites { range, .. } => {
            requirements.instance_batch_count += non_empty(range);
        }
        PrimitiveBatch::Paths {
            rasterization_vertex_count,
            sprite_count,
            ..
        } => {
            // The pinned `ScenePlanRequirements::include_batch` Paths
            // arm: every batch with geometry counts two instance
            // batches (rasterization + sprite) and turns on the path
            // target.
            requirements.path_rasterization_vertex_count += *rasterization_vertex_count as u32;
            if *rasterization_vertex_count > 0 {
                requirements.path_sprite_count += *sprite_count as u32;
                requirements.uses_path_target = 1;
                requirements.instance_batch_count += 2;
            }
        }
        PrimitiveBatch::Surfaces(range) => {
            requirements.surface_count += range.len() as u32;
        }
        PrimitiveBatch::BackdropFilters(range) => {
            requirements.backdrop_filter_count += range.len() as u32;
            requirements.uses_offscreen_target |= u32::from(!range.is_empty());
        }
        PrimitiveBatch::FilterBoundary(_) => {
            // Filter boundaries are compiled before requirements are
            // collected (unreachable, kept for the pinned match shape).
        }
        PrimitiveBatch::EffectBoundary(_) => {
            // Same rule for effect-group markers (rev 16).
        }
    }
}

/// The batch stream over a finished scene, port of the pinned
/// `BatchIterator`. Uses index cursors over the sorted arrays instead of
/// Rust peekable iterators; the merge rules are field-for-field the
/// pinned ones: batches of one kind extend while the next primitive
/// precedes the second-lowest live (order, kind) cursor, and quads and
/// shadows additionally split on corner smoothing.
struct BatchIterator<'a> {
    scene: &'a SceneState,
    shadows_start: usize,
    shadows_cursor: usize,
    quads_start: usize,
    quads_cursor: usize,
    paths_start: usize,
    paths_cursor: usize,
    underlines_start: usize,
    underlines_cursor: usize,
    monochrome_sprites_start: usize,
    monochrome_sprites_cursor: usize,
    subpixel_sprites_start: usize,
    subpixel_sprites_cursor: usize,
    polychrome_sprites_start: usize,
    polychrome_sprites_cursor: usize,
    surfaces_start: usize,
    surfaces_cursor: usize,
    backdrops_start: usize,
    backdrops_cursor: usize,
    boundaries_start: usize,
    boundaries_cursor: usize,
    effects_start: usize,
    effects_cursor: usize,
}

impl<'a> BatchIterator<'a> {
    fn new(scene: &'a SceneState) -> Self {
        Self {
            scene,
            shadows_start: 0,
            shadows_cursor: 0,
            quads_start: 0,
            quads_cursor: 0,
            paths_start: 0,
            paths_cursor: 0,
            underlines_start: 0,
            underlines_cursor: 0,
            monochrome_sprites_start: 0,
            monochrome_sprites_cursor: 0,
            subpixel_sprites_start: 0,
            subpixel_sprites_cursor: 0,
            polychrome_sprites_start: 0,
            polychrome_sprites_cursor: 0,
            surfaces_start: 0,
            surfaces_cursor: 0,
            backdrops_start: 0,
            backdrops_cursor: 0,
            boundaries_start: 0,
            boundaries_cursor: 0,
            effects_start: 0,
            effects_cursor: 0,
        }
    }

    /// The two lowest live (order, kind) cursors in one fixed-size scan
    /// (the pinned `orders_and_kinds` fold).
    fn first_two(&self) -> (Option<(u32, PrimitiveKind)>, Option<(u32, PrimitiveKind)>) {
        let shadows = self.scene.shadows.get(self.shadows_cursor).map(|r| (r.order, PrimitiveKind::Shadow));
        let quads = self.scene.quads.get(self.quads_cursor).map(|r| (r.order, PrimitiveKind::Quad));
        let paths = self.scene.paths.get(self.paths_cursor).map(|data| (data.rec.order, PrimitiveKind::Path));
        let underlines = self.scene.underlines.get(self.underlines_cursor).map(|r| (r.order, PrimitiveKind::Underline));
        let mono_sprites = self.scene.monochrome_sprites.get(self.monochrome_sprites_cursor).map(|r| (r.order, PrimitiveKind::MonochromeSprite));
        let sub_sprites = self.scene.subpixel_sprites.get(self.subpixel_sprites_cursor).map(|r| (r.order, PrimitiveKind::SubpixelSprite));
        let poly_sprites = self.scene.polychrome_sprites.get(self.polychrome_sprites_cursor).map(|r| (r.order, PrimitiveKind::PolychromeSprite));
        let surfaces = self.scene.surfaces.get(self.surfaces_cursor).map(|r| (r.order, PrimitiveKind::Surface));
        let backdrops = self.scene.backdrops.get(self.backdrops_cursor).map(|r| (r.order, PrimitiveKind::BackdropFilter));
        let boundaries = self.scene.boundaries.get(self.boundaries_cursor).map(|r| {
            (
                r.order,
                // The same array yields both start and end markers; the
                // discriminant decides where the marker sorts at an equal
                // order (start before content, end after).
                if r.is_start != 0 {
                    PrimitiveKind::FilterBoundaryStart
                } else {
                    PrimitiveKind::FilterBoundaryEnd
                },
            )
        });
        let effects = self.scene.effects.get(self.effects_cursor).map(|r| {
            (
                r.order,
                // Same rule as the filter markers: the start sorts ahead
                // of the group's content, the end after it (rev 16).
                if (r.flags & effect_flags::IS_START) != 0 {
                    PrimitiveKind::EffectGroupStart
                } else {
                    PrimitiveKind::EffectGroupEnd
                },
            )
        });

        let mut first: Option<(u32, PrimitiveKind)> = None;
        let mut second: Option<(u32, PrimitiveKind)> = None;
        for candidate in [
            shadows,
            quads,
            paths,
            underlines,
            mono_sprites,
            sub_sprites,
            poly_sprites,
            surfaces,
            backdrops,
            boundaries,
            effects,
        ] {
            let Some(candidate) = candidate else { continue };
            if first.is_none_or(|current| candidate < current) {
                second = first;
                first = Some(candidate);
            } else if second.is_none_or(|current| candidate < current) {
                second = Some(candidate);
            }
        }
        (first, second)
    }
}

impl<'a> Iterator for BatchIterator<'a> {
    type Item = PrimitiveBatch;

    fn next(&mut self) -> Option<Self::Item> {
        let (first, second) = self.first_two();
        let (_, batch_kind) = first?;
        let max_order_and_kind = second;

        match batch_kind {
            PrimitiveKind::Shadow => {
                let smoothed = has_corner_smoothing(self.scene.shadows[self.shadows_cursor].corner_smoothing);
                let start = self.shadows_start;
                let mut end = start + 1;
                self.shadows_cursor += 1;
                while let Some(shadow) = self.scene.shadows.get(self.shadows_cursor) {
                    if precedes_limit(shadow.order, batch_kind, max_order_and_kind)
                        && has_corner_smoothing(shadow.corner_smoothing) == smoothed
                    {
                        self.shadows_cursor += 1;
                        end += 1;
                    } else {
                        break;
                    }
                }
                self.shadows_start = end;
                Some(PrimitiveBatch::Shadows { range: start..end, smoothed })
            }
            PrimitiveKind::Quad => {
                let smoothed = has_corner_smoothing(self.scene.quads[self.quads_cursor].corner_smoothing);
                let start = self.quads_start;
                let mut end = start + 1;
                self.quads_cursor += 1;
                while let Some(quad) = self.scene.quads.get(self.quads_cursor) {
                    if precedes_limit(quad.order, batch_kind, max_order_and_kind)
                        && has_corner_smoothing(quad.corner_smoothing) == smoothed
                    {
                        self.quads_cursor += 1;
                        end += 1;
                    } else {
                        break;
                    }
                }
                self.quads_start = end;
                Some(PrimitiveBatch::Quads { range: start..end, smoothed })
            }
            PrimitiveKind::Underline => {
                let start = self.underlines_start;
                let mut end = start + 1;
                self.underlines_cursor += 1;
                while let Some(underline) = self.scene.underlines.get(self.underlines_cursor) {
                    if precedes_limit(underline.order, batch_kind, max_order_and_kind) {
                        self.underlines_cursor += 1;
                        end += 1;
                    } else {
                        break;
                    }
                }
                self.underlines_start = end;
                Some(PrimitiveBatch::Underlines(start..end))
            }
            // Monochrome sprites merge while they stay under the next
            // order/kind limit AND share the batch's atlas texture
            // (the pinned `next_if` condition) and its sampling mode
            // (rev 15: the pixelated split, so one batch draws through
            // one sampler).
            PrimitiveKind::MonochromeSprite => {
                let first = &self.scene.monochrome_sprites[self.monochrome_sprites_cursor];
                let texture_index = first.tile.texture_index;
                let pixelated = first.pixelated != 0;
                let start = self.monochrome_sprites_start;
                let mut end = start + 1;
                self.monochrome_sprites_cursor += 1;
                while let Some(sprite) = self.scene.monochrome_sprites.get(self.monochrome_sprites_cursor) {
                    if precedes_limit(sprite.order, batch_kind, max_order_and_kind)
                        && sprite.tile.texture_index == texture_index
                        && (sprite.pixelated != 0) == pixelated
                    {
                        self.monochrome_sprites_cursor += 1;
                        end += 1;
                    } else {
                        break;
                    }
                }
                self.monochrome_sprites_start = end;
                Some(PrimitiveBatch::MonochromeSprites { texture_index, range: start..end, pixelated })
            }
            PrimitiveKind::SubpixelSprite => {
                let first = &self.scene.subpixel_sprites[self.subpixel_sprites_cursor];
                let texture_index = first.tile.texture_index;
                let pixelated = first.pixelated != 0;
                let start = self.subpixel_sprites_start;
                let mut end = start + 1;
                self.subpixel_sprites_cursor += 1;
                while let Some(sprite) = self.scene.subpixel_sprites.get(self.subpixel_sprites_cursor) {
                    if precedes_limit(sprite.order, batch_kind, max_order_and_kind)
                        && sprite.tile.texture_index == texture_index
                        && (sprite.pixelated != 0) == pixelated
                    {
                        self.subpixel_sprites_cursor += 1;
                        end += 1;
                    } else {
                        break;
                    }
                }
                self.subpixel_sprites_start = end;
                Some(PrimitiveBatch::SubpixelSprites { texture_index, range: start..end, pixelated })
            }
            PrimitiveKind::PolychromeSprite => {
                let first = &self.scene.polychrome_sprites[self.polychrome_sprites_cursor];
                let texture_index = first.tile.texture_index;
                let smoothed = has_corner_smoothing(first.corner_smoothing);
                let pixelated = first.pixelated != 0;
                let start = self.polychrome_sprites_start;
                let mut end = start + 1;
                self.polychrome_sprites_cursor += 1;
                while let Some(sprite) = self.scene.polychrome_sprites.get(self.polychrome_sprites_cursor) {
                    if precedes_limit(sprite.order, batch_kind, max_order_and_kind)
                        && sprite.tile.texture_index == texture_index
                        && has_corner_smoothing(sprite.corner_smoothing) == smoothed
                        && (sprite.pixelated != 0) == pixelated
                    {
                        self.polychrome_sprites_cursor += 1;
                        end += 1;
                    } else {
                        break;
                    }
                }
                self.polychrome_sprites_start = end;
                Some(PrimitiveBatch::PolychromeSprites { texture_index, range: start..end, smoothed, pixelated })
            }
            PrimitiveKind::Surface => {
                let start = self.surfaces_start;
                let mut end = start + 1;
                self.surfaces_cursor += 1;
                while let Some(surface) = self.scene.surfaces.get(self.surfaces_cursor) {
                    if precedes_limit(surface.order, batch_kind, max_order_and_kind) {
                        self.surfaces_cursor += 1;
                        end += 1;
                    } else {
                        break;
                    }
                }
                self.surfaces_start = end;
                Some(PrimitiveBatch::Surfaces(start..end))
            }
            PrimitiveKind::BackdropFilter => {
                let start = self.backdrops_start;
                let mut end = start + 1;
                self.backdrops_cursor += 1;
                while let Some(backdrop) = self.scene.backdrops.get(self.backdrops_cursor) {
                    if precedes_limit(backdrop.order, batch_kind, max_order_and_kind) {
                        self.backdrops_cursor += 1;
                        end += 1;
                    } else {
                        break;
                    }
                }
                self.backdrops_start = end;
                Some(PrimitiveBatch::BackdropFilters(start..end))
            }
            // Boundaries are emitted one at a time (never merged) so the
            // renderer can switch render targets at exactly the right
            // point in the batch stream.
            PrimitiveKind::FilterBoundaryStart | PrimitiveKind::FilterBoundaryEnd => {
                let index = self.boundaries_start;
                self.boundaries_cursor += 1;
                self.boundaries_start = index + 1;
                Some(PrimitiveBatch::FilterBoundary(index))
            }
            // Effect markers likewise (rev 16, ticket 50).
            PrimitiveKind::EffectGroupStart | PrimitiveKind::EffectGroupEnd => {
                let index = self.effects_start;
                self.effects_cursor += 1;
                self.effects_start = index + 1;
                Some(PrimitiveBatch::EffectBoundary(index))
            }
            // Path batches merge while they stay under the next
            // order/kind limit (the pinned `next_if` condition; no
            // smoothing or texture split). The batch carries the summed
            // vertex count and the sprite count: one sprite per path
            // when they share the first's order, else one combined
            // sprite over the union (the pinned computation).
            PrimitiveKind::Path => {
                let start = self.paths_start;
                let mut end = start + 1;
                self.paths_cursor += 1;
                while let Some(path) = self.scene.paths.get(self.paths_cursor) {
                    if precedes_limit(path.rec.order, batch_kind, max_order_and_kind) {
                        self.paths_cursor += 1;
                        end += 1;
                    } else {
                        break;
                    }
                }
                self.paths_start = end;
                let paths = &self.scene.paths[start..end];
                let rasterization_vertex_count: usize =
                    paths.iter().map(|data| data.vertices.len()).sum();
                let sprite_count = if paths
                    .last()
                    .is_some_and(|data| data.rec.order == paths[0].rec.order)
                {
                    paths.len()
                } else {
                    1
                };
                Some(PrimitiveBatch::Paths {
                    range: start..end,
                    rasterization_vertex_count,
                    sprite_count,
                })
            }
        }
    }
}

// ---------------------------------------------------------------------------
// Scene registry
// ---------------------------------------------------------------------------

struct SceneSlot {
    /// Slot generation of the last scene that occupied this slot
    /// (persists while vacant so disposed handles stay stale after slot
    /// reuse).
    slot_generation: u32,
    occupied: bool,
    scene: Option<Arc<SceneShared>>,
}

impl SceneSlot {
    fn vacant() -> Self {
        SceneSlot { slot_generation: 0, occupied: false, scene: None }
    }
}

/// The scene shared between the registry and in-flight calls. `busy` is
/// a coarse re-entry guard: it is set for the whole duration of any
/// native scene call so a same-scene re-entry (a nested call on the same
/// handle from inside a call — impossible through the pure ABI today,
/// but the pattern is preserved) is rejected instead of deadlocking on
/// the state mutex.
struct SceneShared {
    busy: AtomicBool,
    state: Mutex<SceneState>,
}

static SCENES: Mutex<Vec<SceneSlot>> = Mutex::new(Vec::new());

/// Test-only panic probe address bookkeeping (mirrors the bootstrap
/// probe's role: observing real catch_unwind containment through this
/// service's table).
static PANIC_PROBE_INVOCATIONS: AtomicUsize = AtomicUsize::new(0);

fn scenes_lock() -> MutexGuard<'static, Vec<SceneSlot>> {
    SCENES.lock().unwrap_or_else(|poisoned| poisoned.into_inner())
}

fn lock_state(shared: &SceneShared) -> MutexGuard<'_, SceneState> {
    shared.state.lock().unwrap_or_else(|poisoned| poisoned.into_inner())
}

/// Resolves a scene handle to its shared state without holding the
/// registry lock past this function.
fn resolve_scene(handle: u64) -> Result<Arc<SceneShared>, i32> {
    let (slot_index, slot_generation) = scene_handle_parts(handle);
    let registry = scenes_lock();
    if slot_index >= registry.len() {
        return Err(scene_status::ERR_BAD_HANDLE);
    }
    let slot = &registry[slot_index];
    if !slot.occupied || slot.slot_generation != slot_generation {
        return Err(scene_status::ERR_STALE_HANDLE);
    }
    slot.scene
        .clone()
        .ok_or(scene_status::ERR_STALE_HANDLE)
}

/// A scope guard that clears `busy` when the call scope ends, including
/// during unwinding.
struct BusyGuard(Arc<SceneShared>);

impl Drop for BusyGuard {
    fn drop(&mut self) {
        self.0.busy.store(false, Ordering::SeqCst);
    }
}

/// Acquires the scene for a call: resolves the handle, takes the busy
/// flag and locks the state. `f` runs while the state mutex is held; a
/// same-scene re-entry (nested call on the same handle — impossible
/// through this callback-free ABI, but the service pattern is preserved)
/// is rejected with [`scene_status::ERR_BAD_VALUE`] before the lock.
fn with_scene<R>(
    handle: u64,
    f: impl FnOnce(&mut SceneState) -> Result<R, i32>,
) -> Result<R, i32> {
    let shared = resolve_scene(handle)?;
    if shared.busy.swap(true, Ordering::SeqCst) {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    let _busy_guard = BusyGuard(shared.clone());
    let mut state = lock_state(&shared);
    if state.failed {
        return Err(scene_status::ERR_SCENE_FAILED);
    }
    f(&mut state)
}

/// Marks a scene failed after a contained panic (best-effort).
fn mark_scene_failed(handle: u64) {
    if let Ok(shared) = resolve_scene(handle) {
        lock_state(&shared).failed = true;
    }
}

// ---------------------------------------------------------------------------
// Record validation
// ---------------------------------------------------------------------------

fn check_record_size(actual: u32, expected: u32) -> Result<(), i32> {
    if actual != expected {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    Ok(())
}

fn check_color(color: &GpuiGoSceneColor) -> Result<(), i32> {
    if color.tag != 0 {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    Ok(())
}

/// Validates a background record (rev 15): a known tag and color space,
/// finite gradient scalars, non-degenerate stop positions, and a
/// zeroed unused arm (the surface record's strict-payload pattern).
///
/// rev 16, ticket 52: tag 4 is the R16 gradient-table arm - the
/// background references a scene-level gradient entry by index (the
/// index stored in the ANGLE field as f32 bits; the plain sRGB space
/// only, the table record carries its own). The arm is allowed on
/// quads and shadows (the vendored pipelines evaluate it) and REJECTED
/// on paths (`allow_table` false: the pinned path-sprite pipeline
/// consumes the pinned DXBC and cannot sample the table; the
/// record-size self-check plus this gate keep the ABI honest).
fn check_background_with(bg: &GpuiGoSceneBackground, allow_table: bool) -> Result<(), i32> {
    if bg.reserved != 0 {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    match (bg.tag, bg.color_space) {
        (0, _) => {
            let gradient_words = [
                bg.angle,
                bg.stop0_h,
                bg.stop0_s,
                bg.stop0_l,
                bg.stop0_a,
                bg.stop0_percentage,
                bg.stop1_h,
                bg.stop1_s,
                bg.stop1_l,
                bg.stop1_a,
                bg.stop1_percentage,
            ];
            if gradient_words != [0u32; 11] {
                return Err(scene_status::ERR_BAD_VALUE);
            }
            if bg.color_space > 1 {
                return Err(scene_status::ERR_BAD_VALUE);
            }
            Ok(())
        }
        (1, space) if space <= 1 => {
            let angle = f32::from_bits(bg.angle);
            if !angle.is_finite() {
                return Err(scene_status::ERR_BAD_VALUE);
            }
            for bits in [
                bg.stop0_h,
                bg.stop0_s,
                bg.stop0_l,
                bg.stop0_a,
                bg.stop0_percentage,
                bg.stop1_h,
                bg.stop1_s,
                bg.stop1_l,
                bg.stop1_a,
                bg.stop1_percentage,
            ] {
                if !f32::from_bits(bits).is_finite() {
                    return Err(scene_status::ERR_BAD_VALUE);
                }
            }
            // The shader divides by the stop delta; CSS's
            // equal-position hard stop needs the n-stop form (R16).
            if f32::from_bits(bg.stop0_percentage) == f32::from_bits(bg.stop1_percentage) {
                return Err(scene_status::ERR_BAD_VALUE);
            }
            let solid_words = [bg.h, bg.s, bg.l, bg.a];
            if solid_words != [0u32; 4] {
                return Err(scene_status::ERR_BAD_VALUE);
            }
            Ok(())
        }
        (4, 0) if allow_table => {
            // The gradient table index rides the ANGLE field as f32
            // bits: finite, non-negative, integral, and under the
            // table bound (the RENDERER resolves the reference - an
            // out-of-range index paints transparent, the design's
            // "an invalid mask hides the element" rule applied to
            // gradients - so replayed ranges stay robust).
            let index = f32::from_bits(bg.angle);
            if !index.is_finite()
                || index < 0.0
                || index != index.trunc()
                || index >= MAX_SCENE_GRADIENTS as f32
            {
                return Err(scene_status::ERR_BAD_VALUE);
            }
            let others = [
                bg.stop0_h,
                bg.stop0_s,
                bg.stop0_l,
                bg.stop0_a,
                bg.stop0_percentage,
                bg.stop1_h,
                bg.stop1_s,
                bg.stop1_l,
                bg.stop1_a,
                bg.stop1_percentage,
            ];
            if others != [0u32; 10] {
                return Err(scene_status::ERR_BAD_VALUE);
            }
            let solid_words = [bg.h, bg.s, bg.l, bg.a];
            if solid_words != [0u32; 4] {
                return Err(scene_status::ERR_BAD_VALUE);
            }
            Ok(())
        }
        _ => Err(scene_status::ERR_BAD_VALUE),
    }
}

fn check_background(bg: &GpuiGoSceneBackground) -> Result<(), i32> {
    check_background_with(bg, true)
}

fn check_bool01(value: u32) -> Result<(), i32> {
    if value > 1 {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    Ok(())
}

/// The extended content mask's validation (rev 16, ticket 50): the
/// fade distances and corner radii must be finite and non-negative,
/// the smoothing in [0, 1]. A zero-everything mask (the pre-R16 shape)
/// always passes.
fn check_content_mask(mask: &GpuiGoSceneContentMask) -> Result<(), i32> {
    for bits in mask.fade_out.iter() {
        let fade = f32::from_bits(*bits);
        if !fade.is_finite() || fade < 0.0 {
            return Err(scene_status::ERR_BAD_VALUE);
        }
    }
    for bits in mask.corner_radii.iter() {
        let radius = f32::from_bits(*bits);
        if !radius.is_finite() || radius < 0.0 {
            return Err(scene_status::ERR_BAD_VALUE);
        }
    }
    let smoothing = f32::from_bits(mask.corner_smoothing);
    if !smoothing.is_finite() || !(0.0..=1.0).contains(&smoothing) {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    Ok(())
}

/// The effect-group record's validation (rev 16, ticket 50; the design
/// appendix A.1 rules): known flag bits only, an invertible 2x2 when
/// `has_transform` is set, `mask_kind` 0 in this revision (the
/// gradient/atlas/coverage tables arrive with ticket 52), the
/// property-node id non-zero and paired with a transform or opacity,
/// the opacity in [0, 1], finite radii and smoothing in [0, 1].
fn check_effect(rec: &GpuiGoSceneEffectRecord) -> Result<(), i32> {
    check_record_size(rec.record_size, core::mem::size_of::<GpuiGoSceneEffectRecord>() as u32)?;
    if rec.order != 0 {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    if rec.flags & !effect_flags::ALL != 0 {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    if (rec.flags & effect_flags::HAS_TRANSFORM) != 0 {
        let (m00, m01) = (f32::from_bits(rec.transform[0]), f32::from_bits(rec.transform[1]));
        let (m10, m11) = (f32::from_bits(rec.transform[2]), f32::from_bits(rec.transform[3]));
        for value in [m00, m01, m10, m11] {
            if !value.is_finite() {
                return Err(scene_status::ERR_BAD_VALUE);
            }
        }
        let determinant = m00 * m11 - m01 * m10;
        if !determinant.is_finite() || determinant == 0.0 {
            return Err(scene_status::ERR_BAD_VALUE);
        }
    }
    if rec.blend_mode > 16 {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    // rev 16, ticket 52: the mask kinds 1-3 are wired - the composite
    // samples the gradient table / the atlas texture / the coverage
    // target at render. The REFERENCES are resolved by the renderer
    // (an unresolvable mask hides the element - the design §3.5's
    // transparent-black rule), NOT validated here: a replayed range
    // can carry an effect record whose table entry lives outside the
    // range, and the render-time resolution keeps that robust.
    if rec.mask_kind > 3 {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    if rec.mask_kind == 0 && (rec.mask_reference != 0 || rec.mask_mode != 0) {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    if rec.mask_kind != 0 && rec.mask_mode > 1 {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    if (rec.flags & effect_flags::PROPERTY_NODE) != 0 {
        if rec.node_id == 0 {
            return Err(scene_status::ERR_BAD_VALUE);
        }
        if (rec.flags & (effect_flags::HAS_TRANSFORM | effect_flags::HAS_OPACITY)) == 0 {
            return Err(scene_status::ERR_BAD_VALUE);
        }
    }
    let opacity = f32::from_bits(rec.opacity);
    if !opacity.is_finite() || !(0.0..=1.0).contains(&opacity) {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    for bits in rec.corner_radii.iter() {
        let radius = f32::from_bits(*bits);
        if !radius.is_finite() || radius < 0.0 {
            return Err(scene_status::ERR_BAD_VALUE);
        }
    }
    let smoothing = f32::from_bits(rec.corner_smoothing);
    if !smoothing.is_finite() || !(0.0..=1.0).contains(&smoothing) {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    if rec.reserved != 0 {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    Ok(())
}

/// The effect group's 2x3 matrix, row-major `[m00, m01, m10, m11, tx,
/// ty]` (the record's `transform` field, f32 bits). Maps LOCAL
/// (untransformed window-space) points to device points: `device =
/// M·p + t` with the transform-origin resolved by the caller (rev 16,
/// ticket 51).
pub(crate) fn effect_matrix(rec: &GpuiGoSceneEffectRecord) -> [f32; 6] {
    [
        f32::from_bits(rec.transform[0]),
        f32::from_bits(rec.transform[1]),
        f32::from_bits(rec.transform[2]),
        f32::from_bits(rec.transform[3]),
        f32::from_bits(rec.transform[4]),
        f32::from_bits(rec.transform[5]),
    ]
}

/// The effect group's translation `(tx, ty)` (the matrix's last
/// column).
pub(crate) fn effect_translation(rec: &GpuiGoSceneEffectRecord) -> (f32, f32) {
    (f32::from_bits(rec.transform[4]), f32::from_bits(rec.transform[5]))
}

/// Whether the effect group's 2x2 is the identity (a translation-only
/// transform - the design §3.4 fast-path economy case). Non-finite
/// matrices never reach here (the insert validation rejects them).
pub(crate) fn effect_translation_only(rec: &GpuiGoSceneEffectRecord) -> bool {
    let [m00, m01, m10, m11, _, _] = effect_matrix(rec);
    m00 == 1.0 && m11 == 1.0 && m01 == 0.0 && m10 == 0.0
}

/// Validates a gradient table entry (rev 16, ticket 52): known kind and
/// tile tags, the sRGB space (Oklab is reserved), finite geometry with
/// the kind's non-degeneracy rule, a stop count in 1..=64, and stops
/// that are finite, ordered non-decreasing within [0, 1] (the CSS
/// fixup is the CALLER's; the service interpolates the fixed stops).
/// `stop_row` must be zero on insert (the service assigns it).
fn check_gradient(
    rec: &GpuiGoSceneGradientRecord,
    stops: &[GpuiGoSceneGradientStop],
) -> Result<(), i32> {
    check_record_size(
        rec.record_size,
        core::mem::size_of::<GpuiGoSceneGradientRecord>() as u32,
    )?;
    if rec.kind > gradient_kind::CONIC {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    if rec.tile > gradient_tile::REFLECT {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    if rec.color_space != 0 {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    for bits in rec.geometry.iter() {
        if !f32::from_bits(*bits).is_finite() {
            return Err(scene_status::ERR_BAD_VALUE);
        }
    }
    match rec.kind {
        gradient_kind::LINEAR => {
            // The parameter divides by the line's length.
            let (x0, y0) = (f32::from_bits(rec.geometry[0]), f32::from_bits(rec.geometry[1]));
            let (x1, y1) = (f32::from_bits(rec.geometry[2]), f32::from_bits(rec.geometry[3]));
            if (x1 - x0) == 0.0 && (y1 - y0) == 0.0 {
                return Err(scene_status::ERR_BAD_VALUE);
            }
        }
        gradient_kind::RADIAL => {
            // The radii must differ (the parameter divides by the
            // delta) and the aspect ratio must be positive.
            let (r0, r1) = (f32::from_bits(rec.geometry[2]), f32::from_bits(rec.geometry[3]));
            let aspect = f32::from_bits(rec.geometry[4]);
            if r1 == r0 || aspect <= 0.0 {
                return Err(scene_status::ERR_BAD_VALUE);
            }
        }
        gradient_kind::CONIC => {
            // The sweep must be positive (the parameter divides by it).
            let rotation = f32::from_bits(rec.geometry[3]);
            if rotation <= 0.0 {
                return Err(scene_status::ERR_BAD_VALUE);
            }
        }
        _ => unreachable!("the kind tag is validated above"),
    }
    if rec.stop_row != 0 {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    if stops.is_empty() || stops.len() > MAX_GRADIENT_STOPS as usize {
        return Err(scene_status::ERR_LIMIT);
    }
    if rec.stop_count != stops.len() as u32 {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    let mut previous = f32::NEG_INFINITY;
    for stop in stops {
        let position = f32::from_bits(stop.position);
        if !position.is_finite() || !(0.0..=1.0).contains(&position) {
            return Err(scene_status::ERR_BAD_VALUE);
        }
        if position < previous {
            return Err(scene_status::ERR_BAD_VALUE);
        }
        previous = position;
    }
    Ok(())
}

/// Bakes one gradient's stops into a 1024x4-byte premultiplied-sRGB
/// ramp row (rev 16, ticket 52): texel i holds the interpolation at
/// t = i/1023 - the stops' straight sRGBA bytes premultiplied, mixed
/// linearly in f32, and rounded back to bytes (the CPU reference in
/// the spec tests mirrors this arithmetic exactly; the hardware's
/// linear filtering of the baked bytes IS CSS's interpolation rule,
/// the design §3.8's ramp-row decision). Before the first stop the
/// first color continues; after the last the last (the pad tile's
/// behavior baked into the row).
fn bake_gradient_ramp(stops: &[GpuiGoSceneGradientStop]) -> Vec<u8> {
    let mut ramp = vec![0u8; GRADIENT_RAMP_WIDTH as usize * 4];
    let lanes = |stop: &GpuiGoSceneGradientStop| -> [f32; 4] {
        // Straight sRGBA bytes to normalized f32 lanes (r, g, b, a).
        [
            (stop.rgba & 0xFF) as f32 / 255.0,
            ((stop.rgba >> 8) & 0xFF) as f32 / 255.0,
            ((stop.rgba >> 16) & 0xFF) as f32 / 255.0,
            ((stop.rgba >> 24) & 0xFF) as f32 / 255.0,
        ]
    };
    // The stop's PREMULTIPLIED sRGB lanes (r*a, g*a, b*a, a).
    let premultiplied = |stop: &GpuiGoSceneGradientStop| -> [f32; 4] {
        let l = lanes(stop);
        [l[0] * l[3], l[1] * l[3], l[2] * l[3], l[3]]
    };
    let position = |stop: &GpuiGoSceneGradientStop| f32::from_bits(stop.position);
    for texel in 0..GRADIENT_RAMP_WIDTH as usize {
        let t = texel as f32 / (GRADIENT_RAMP_WIDTH - 1) as f32;
        let mut color = premultiplied(&stops[0]);
        if t <= position(&stops[0]) {
            color = premultiplied(&stops[0]);
        } else if t >= position(&stops[stops.len() - 1]) {
            color = premultiplied(&stops[stops.len() - 1]);
        } else {
            // The largest stop index whose position is <= t (the
            // interval's left end; the delta to the next stop is
            // strictly positive by construction).
            let mut index = 0usize;
            for candidate in 1..stops.len() {
                if position(&stops[candidate]) <= t {
                    index = candidate;
                }
            }
            let p0 = position(&stops[index]);
            let p1 = position(&stops[index + 1]);
            let factor = (t - p0) / (p1 - p0);
            let left = premultiplied(&stops[index]);
            let right = premultiplied(&stops[index + 1]);
            for lane in 0..4 {
                color[lane] = left[lane] + factor * (right[lane] - left[lane]);
            }
        }
        for lane in 0..4 {
            let scaled = color[lane] * 255.0 + 0.5;
            let byte = if scaled < 0.0 {
                0u8
            } else if scaled > 255.0 {
                255u8
            } else {
                scaled as u8
            };
            ramp[texel * 4 + lane] = byte;
        }
    }
    ramp
}

/// Validates a coverage-mask entry (rev 16, ticket 52): finite bounds,
/// a non-negative path index (the RENDERER resolves the reference -
/// an out-of-range path index renders the mask empty, hiding the
/// element).
fn check_coverage(rec: &GpuiGoSceneCoverageRecord) -> Result<(), i32> {
    check_record_size(
        rec.record_size,
        core::mem::size_of::<GpuiGoSceneCoverageRecord>() as u32,
    )?;
    for bits in [rec.bounds.x, rec.bounds.y, rec.bounds.w, rec.bounds.h] {
        if !f32::from_bits(bits).is_finite() {
            return Err(scene_status::ERR_BAD_VALUE);
        }
    }
    Ok(())
}

/// Validates one filter-op record (rev 16, ticket 52): the
/// drop-shadow kind only (the blur/colour-matrix tags are R15's chain
/// semantics, re-stated in the appendix for reference - an op of
/// those kinds is not insertable), finite offset and radius, a solid
/// color record.
fn check_filter_op(op: &GpuiGoSceneFilterOpRecord) -> Result<(), i32> {
    check_record_size(
        op.record_size,
        core::mem::size_of::<GpuiGoSceneFilterOpRecord>() as u32,
    )?;
    if op.kind != filter_op_kind::DROP_SHADOW {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    for bits in op.offset.iter() {
        if !f32::from_bits(*bits).is_finite() {
            return Err(scene_status::ERR_BAD_VALUE);
        }
    }
    let radius = f32::from_bits(op.blur_radius);
    if !radius.is_finite() || radius < 0.0 {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    check_color(&op.color)?;
    Ok(())
}

/// Maps a window-space rect's four corners through the 2x3 matrix
/// `[m00, m01, m10, m11, tx, ty]` (device = M·p + t) and returns the
/// mapped footprint's axis-aligned bounds - the group's device-space
/// composite region, the extent pre-pass's footprint and the
/// `scene_update_property` bounds answer.
pub(crate) fn map_bounds_corners(bounds: [f32; 4], matrix: [f32; 6]) -> [f32; 4] {
    let [m00, m01, m10, m11, tx, ty] = matrix;
    let corners = [
        (bounds[0], bounds[1]),
        (bounds[0] + bounds[2], bounds[1]),
        (bounds[0], bounds[1] + bounds[3]),
        (bounds[0] + bounds[2], bounds[1] + bounds[3]),
    ];
    let mut min_x = f32::INFINITY;
    let mut min_y = f32::INFINITY;
    let mut max_x = f32::NEG_INFINITY;
    let mut max_y = f32::NEG_INFINITY;
    for (x, y) in corners {
        let dx = m00 * x + m01 * y + tx;
        let dy = m10 * x + m11 * y + ty;
        min_x = min_x.min(dx);
        min_y = min_y.min(dy);
        max_x = max_x.max(dx);
        max_y = max_y.max(dy);
    }
    [min_x, min_y, max_x - min_x, max_y - min_y]
}

fn check_quad(rec: &GpuiGoSceneQuadRecord) -> Result<(), i32> {
    check_record_size(rec.record_size, core::mem::size_of::<GpuiGoSceneQuadRecord>() as u32)?;
    if rec.order != 0 {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    if rec.border_style > 1 {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    check_background(&rec.background)?;
    check_background(&rec.border_color)?;
    check_content_mask(&rec.content_mask)?;
    Ok(())
}

fn check_shadow(rec: &GpuiGoSceneShadowRecord) -> Result<(), i32> {
    check_record_size(rec.record_size, core::mem::size_of::<GpuiGoSceneShadowRecord>() as u32)?;
    if rec.order != 0 {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    check_bool01(rec.inset)?;
    check_background(&rec.color)?;
    check_content_mask(&rec.content_mask)?;
    Ok(())
}

fn check_underline(rec: &GpuiGoSceneUnderlineRecord) -> Result<(), i32> {
    check_record_size(
        rec.record_size,
        core::mem::size_of::<GpuiGoSceneUnderlineRecord>() as u32,
    )?;
    if rec.order != 0 {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    check_bool01(rec.wavy)?;
    check_color(&rec.color)?;
    check_content_mask(&rec.content_mask)?;
    Ok(())
}

fn check_filter(rec: &GpuiGoSceneFilterRecord) -> Result<(), i32> {
    check_record_size(rec.record_size, core::mem::size_of::<GpuiGoSceneFilterRecord>() as u32)?;
    if rec.order != 0 {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    check_bool01(rec.is_start)?;
    if rec.filter_count > MAX_SCENE_FILTERS {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    // rev 15 / rev 15.1: the per-filter tags and the per-function
    // colour matrices. Every used tag must be a known kind; the tag-1
    // entries must match the matrix count; the matrix payload must be
    // finite (the carried slots); an unused tag or a zero matrix count
    // demands zeroed payload words.
    let mut matrix_entries = 0u32;
    for (index, tag) in rec.filter_tags.iter().enumerate() {
        if (index as u32) < rec.filter_count {
            match *tag {
                0 => {
                    let radius = f32::from_bits(rec.blur_radii[index]);
                    if !radius.is_finite() || radius < 0.0 {
                        return Err(scene_status::ERR_BAD_VALUE);
                    }
                }
                1 => {
                    matrix_entries += 1;
                    if rec.blur_radii[index] != 0 {
                        return Err(scene_status::ERR_BAD_VALUE);
                    }
                }
                _ => return Err(scene_status::ERR_BAD_VALUE),
            }
        } else if *tag != 0 {
            return Err(scene_status::ERR_BAD_VALUE);
        }
    }
    if rec.color_matrix_count != matrix_entries {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    let carried = (rec.color_matrix_count as usize) * 20;
    if rec.color_matrix_count == 0 {
        if rec.color_matrix != [0u32; 160] {
            return Err(scene_status::ERR_BAD_VALUE);
        }
    } else {
        for bits in &rec.color_matrix[..carried] {
            if !f32::from_bits(*bits).is_finite() {
                return Err(scene_status::ERR_BAD_VALUE);
            }
        }
        // Slots at and beyond the count must be zeroed.
        if rec.color_matrix[carried..].iter().any(|&bits| bits != 0) {
            return Err(scene_status::ERR_BAD_VALUE);
        }
    }
    // rev 16, ticket 52: the appended drop-shadow op-chain reference -
    // 0/0 is the R15 legacy shape; a count demands an index (the chain
    // itself is resolved at SNAPSHOT time: an out-of-range index reads
    // as an empty chain, the R15 semantics, so replayed ranges stay
    // robust).
    if rec.op_chain_count == 0 && rec.op_chain_index != 0 {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    if rec.op_chain_count > MAX_SCENE_FILTER_OPS {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    check_content_mask(&rec.content_mask)?;
    Ok(())
}

fn check_surface(rec: &GpuiGoSceneSurfaceRecord) -> Result<(), i32> {
    check_record_size(rec.record_size, core::mem::size_of::<GpuiGoSceneSurfaceRecord>() as u32)?;
    if rec.order != 0 {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    // The source payload: tag 0 (Unsupported) demands the zeroed
    // reserved words; tag 1 (the capture frame import, ticket28)
    // carries the 64-bit frame handle packed across reserved[0]
    // (low 32 bits) and reserved[1] (high 32 bits), with reserved[2]
    // zero. The rev-10 artifact rejected every tag-1 payload at this
    // gate (the state-level insert and the renderer draw path accepted
    // them); the rev-11 fix aligns the export gate with the state
    // level.
    match rec.source_tag {
        surface_source_kind::UNSUPPORTED => {
            if rec.source_reserved != [0u32; 3] {
                return Err(scene_status::ERR_BAD_VALUE);
            }
        }
        surface_source_kind::CAPTURE_FRAME => {
            if rec.source_reserved[0] == 0 || rec.source_reserved[2] != 0 {
                return Err(scene_status::ERR_BAD_VALUE);
            }
        }
        _ => return Err(scene_status::ERR_BAD_VALUE),
    }
    check_content_mask(&rec.content_mask)?;
    Ok(())
}

/// Validates a monochrome/subpixel sprite record: record size, zero
/// order, finite color/transformation floats, and the tile's texture
/// kind matching the sprite class's pool (a tile from another pool would
/// be reinterpreted by the draw pipeline — the renderer contract's
/// "never reinterpret one format as another").
fn check_mask_sprite(rec: &GpuiGoSceneSpriteRecord, class: SpriteClass) -> Result<(), i32> {
    check_record_size(rec.record_size, core::mem::size_of::<GpuiGoSceneSpriteRecord>() as u32)?;
    if rec.order != 0 {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    check_color(&rec.color)?;
    for bits in rec.transformation {
        if !f32::from_bits(bits).is_finite() {
            return Err(scene_status::ERR_BAD_VALUE);
        }
    }
    check_tile(&rec.tile, class)?;
    check_content_mask(&rec.content_mask)
}

/// Validates a polychrome sprite record.    check_content_mask(&rec.content_mask)?;

fn check_polychrome_sprite(rec: &GpuiGoScenePolychromeSpriteRecord) -> Result<(), i32> {
    check_record_size(
        rec.record_size,
        core::mem::size_of::<GpuiGoScenePolychromeSpriteRecord>() as u32,
    )?;
    if rec.order != 0 {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    if rec.grayscale > 1 {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    if !f32::from_bits(rec.opacity).is_finite()
        || !f32::from_bits(rec.corner_smoothing).is_finite()
    {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    for bits in rec.corner_radii {
        if !f32::from_bits(bits).is_finite() {
            return Err(scene_status::ERR_BAD_VALUE);
        }
    }
    check_tile(&rec.tile, SpriteClass::Polychrome)?;
    check_content_mask(&rec.content_mask)
}

fn check_tile(tile: &GpuiGoSceneTileRecord, class: SpriteClass) -> Result<(), i32> {
    if tile.texture_kind != tile.expected_texture_kind(class) {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    if tile.bounds_w <= 0 || tile.bounds_h <= 0 {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    if tile.bounds_x < 0 || tile.bounds_y < 0 {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    Ok(())
}

// ---------------------------------------------------------------------------
// Export bodies
// ---------------------------------------------------------------------------

fn scene_create_body(out_handle: *mut u64) -> i32 {
    if out_handle.is_null() {
        return scene_status::ERR_NULL_ARG;
    }
    let mut registry = scenes_lock();
    let slot_index = match registry.iter().position(|slot| !slot.occupied) {
        Some(index) => index,
        None => {
            registry.push(SceneSlot::vacant());
            registry.len() - 1
        }
    };
    let slot_generation = match registry[slot_index].slot_generation.checked_add(1) {
        Some(generation) if generation <= SCENE_GENERATION_MAX => generation,
        _ => return scene_status::ERR_BAD_VALUE,
    };
    let shared = Arc::new(SceneShared {
        busy: AtomicBool::new(false),
        state: Mutex::new(SceneState::default()),
    });
    registry[slot_index] = SceneSlot { slot_generation, occupied: true, scene: Some(shared) };
    unsafe { *out_handle = make_scene_handle(slot_index, slot_generation) };
    scene_status::OK
}

fn scene_clear_body(handle: u64) -> i32 {
    match with_scene(handle, |state| {
        state.clear();
        Ok(())
    }) {
        Ok(()) => scene_status::OK,
        Err(code) => code,
    }
}

fn scene_dispose_body(handle: u64) -> i32 {
    let (slot_index, slot_generation) = scene_handle_parts(handle);
    let mut registry = scenes_lock();
    if slot_index >= registry.len() {
        return scene_status::ERR_BAD_HANDLE;
    }
    let slot = &mut registry[slot_index];
    if !slot.occupied || slot.slot_generation != slot_generation {
        return scene_status::ERR_STALE_HANDLE;
    }
    let shared = match slot.scene.clone() {
        Some(shared) => shared,
        None => return scene_status::ERR_STALE_HANDLE,
    };
    if shared.busy.load(Ordering::SeqCst) {
        // Disposal happens only after active native calls return.
        return scene_status::ERR_BAD_VALUE;
    }
    slot.occupied = false;
    slot.scene = None;
    drop(registry);
    // Drop our reference after the registry lock is released; in-flight
    // callers hold their own clones (deferred native destruction).
    drop(shared);
    scene_status::OK
}

fn scene_push_layer_body(handle: u64, bounds: *const GpuiGoSceneBounds) -> i32 {
    if bounds.is_null() {
        return scene_status::ERR_NULL_ARG;
    }
    let bounds_rec: &GpuiGoSceneBounds = unsafe { &*bounds };
    let f32_bounds = bounds_rec.to_f32();
    match with_scene(handle, |state| {
        state.push_layer(f32_bounds)
    }) {
        Ok(()) => scene_status::OK,
        Err(code) => code,
    }
}

fn scene_pop_layer_body(handle: u64) -> i32 {
    match with_scene(handle, |state| state.pop_layer()) {
        Ok(()) => scene_status::OK,
        Err(code) => code,
    }
}

fn scene_raise_order_floor_body(handle: u64) -> i32 {
    match with_scene(handle, |state| {
        state.raise_order_floor()
    }) {
        Ok(()) => scene_status::OK,
        Err(code) => code,
    }
}

fn scene_insert_quad_body(handle: u64, rec: *const GpuiGoSceneQuadRecord) -> i32 {
    if rec.is_null() {
        return scene_status::ERR_NULL_ARG;
    }
    let record: &GpuiGoSceneQuadRecord = unsafe { &*rec };
    if let Err(code) = check_quad(record) {
        return code;
    }
    let record = *record;
    match with_scene(handle, |state| state.insert_quad(record)) {
        Ok(()) => scene_status::OK,
        Err(code) => code,
    }
}

fn scene_insert_shadow_body(handle: u64, rec: *const GpuiGoSceneShadowRecord) -> i32 {
    if rec.is_null() {
        return scene_status::ERR_NULL_ARG;
    }
    let record: &GpuiGoSceneShadowRecord = unsafe { &*rec };
    if let Err(code) = check_shadow(record) {
        return code;
    }
    let record = *record;
    match with_scene(handle, |state| state.insert_shadow(record)) {
        Ok(()) => scene_status::OK,
        Err(code) => code,
    }
}

fn scene_insert_underline_body(handle: u64, rec: *const GpuiGoSceneUnderlineRecord) -> i32 {
    if rec.is_null() {
        return scene_status::ERR_NULL_ARG;
    }
    let record: &GpuiGoSceneUnderlineRecord = unsafe { &*rec };
    if let Err(code) = check_underline(record) {
        return code;
    }
    let record = *record;
    match with_scene(handle, |state| state.insert_underline(record)) {
        Ok(()) => scene_status::OK,
        Err(code) => code,
    }
}

fn scene_insert_backdrop_body(handle: u64, rec: *const GpuiGoSceneFilterRecord) -> i32 {
    if rec.is_null() {
        return scene_status::ERR_NULL_ARG;
    }
    let record: &GpuiGoSceneFilterRecord = unsafe { &*rec };
    if let Err(code) = check_filter(record) {
        return code;
    }
    let record = *record;
    match with_scene(handle, |state| {
        state.insert_backdrop(record)
    }) {
        Ok(()) => scene_status::OK,
        Err(code) => code,
    }
}

fn scene_insert_boundary_body(handle: u64, rec: *const GpuiGoSceneFilterRecord) -> i32 {
    if rec.is_null() {
        return scene_status::ERR_NULL_ARG;
    }
    let record: &GpuiGoSceneFilterRecord = unsafe { &*rec };
    if let Err(code) = check_filter(record) {
        return code;
    }
    let record = *record;
    match with_scene(handle, |state| {
        state.insert_boundary(record)
    }) {
        Ok(()) => scene_status::OK,
        Err(code) => code,
    }
}

fn scene_insert_effect_body(handle: u64, rec: *const GpuiGoSceneEffectRecord) -> i32 {
    if rec.is_null() {
        return scene_status::ERR_NULL_ARG;
    }
    let result = with_scene(handle, |state| {
        let record = unsafe { *rec };
        check_effect(&record)?;
        state.insert_effect(record)?;
        Ok(())
    });
    match result {
        Ok(()) => scene_status::OK,
        Err(code) => code,
    }
}

/// Dump kinds of `scene_dump` (the effect records travel through the
/// dedicated `scene_effect_dump` entry instead).
fn scene_effect_dump_body(
    handle: u64,
    records: *mut GpuiGoSceneEffectRecord,
    capacity: u32,
    out_count: *mut u32,
) -> i32 {
    if out_count.is_null() {
        return scene_status::ERR_NULL_ARG;
    }
    let result = with_scene(handle, |state| {
        if !state.is_finished {
            return Err(scene_status::ERR_NOT_FINISHED);
        }
        let needed = state.effects.len() as u32;
        unsafe { *out_count = needed };
        if needed > capacity {
            return Err(scene_status::ERR_CAPACITY);
        }
        if capacity > 0 && records.is_null() {
            return Err(scene_status::ERR_NULL_ARG);
        }
        if capacity > 0 {
            let out = unsafe { std::slice::from_raw_parts_mut(records, capacity as usize) };
            for (slot, effect) in out.iter_mut().zip(state.effects.iter()) {
                *slot = *effect;
            }
        }
        Ok(())
    });
    match result {
        Ok(()) => scene_status::OK,
        Err(code) => code,
    }
}

/// `scene_insert_gradient` (rev 16, ticket 52): one gradient table
/// entry plus its bulk stop array. The service bakes the 1024x1
/// premultiplied-sRGB ramp row and assigns `stop_row` (the gradient's
/// own table index); the renderer uploads the rows as the ramp
/// texture. Returns the assigned gradient index through `out_index`
/// (null allowed - no report).
fn scene_insert_gradient_body(
    handle: u64,
    rec: *const GpuiGoSceneGradientRecord,
    stops: *const GpuiGoSceneGradientStop,
    stop_count: u32,
    out_index: *mut u32,
) -> i32 {
    if rec.is_null() {
        return scene_status::ERR_NULL_ARG;
    }
    if stop_count > 0 && stops.is_null() {
        return scene_status::ERR_NULL_ARG;
    }
    let stop_slice: &[GpuiGoSceneGradientStop] = if stop_count == 0 {
        &[]
    } else {
        unsafe { std::slice::from_raw_parts(stops, stop_count as usize) }
    };
    let result = with_scene(handle, |state| {
        let record = unsafe { *rec };
        check_gradient(&record, stop_slice)?;
        let index = state.gradients.len() as u32;
        state.insert_gradient(record, stop_slice)?;
        if !out_index.is_null() {
            unsafe { *out_index = index };
        }
        Ok(())
    });
    match result {
        Ok(()) => scene_status::OK,
        Err(code) => code,
    }
}

/// `scene_gradient_dump` (rev 16, ticket 52): the finished scene's
/// gradient records and the concatenated stop array (the
/// `path_dump` capacity protocol - both capacities are checked with
/// the required counts reported in the out parameters; the baked ramp
/// rows are renderer-internal and verified through the readback
/// tests).
fn scene_gradient_dump_body(
    handle: u64,
    records: *mut GpuiGoSceneGradientRecord,
    stops: *mut GpuiGoSceneGradientStop,
    record_capacity: u32,
    stop_capacity: u32,
    out_record_count: *mut u32,
    out_stop_count: *mut u32,
) -> i32 {
    if out_record_count.is_null() || out_stop_count.is_null() {
        return scene_status::ERR_NULL_ARG;
    }
    let result = with_scene(handle, |state| {
        if !state.is_finished {
            return Err(scene_status::ERR_NOT_FINISHED);
        }
        let record_count = state.gradients.len() as u32;
        let stop_count: u32 = state.gradients.iter().map(|g| g.stops.len() as u32).sum();
        unsafe {
            *out_record_count = record_count;
            *out_stop_count = stop_count;
        }
        if record_count > record_capacity || stop_count > stop_capacity {
            return Err(scene_status::ERR_CAPACITY);
        }
        if record_count > 0 && records.is_null() {
            return Err(scene_status::ERR_NULL_ARG);
        }
        if stop_count > 0 && stops.is_null() {
            return Err(scene_status::ERR_NULL_ARG);
        }
        if record_count > 0 {
            let out =
                unsafe { std::slice::from_raw_parts_mut(records, record_capacity as usize) };
            for (slot, gradient) in out.iter_mut().zip(state.gradients.iter()) {
                *slot = gradient.rec;
            }
        }
        if stop_count > 0 {
            let out =
                unsafe { std::slice::from_raw_parts_mut(stops, stop_capacity as usize) };
            let mut cursor = 0usize;
            for gradient in state.gradients.iter() {
                for stop in gradient.stops.iter() {
                    out[cursor] = *stop;
                    cursor += 1;
                }
            }
        }
        Ok(())
    });
    match result {
        Ok(()) => scene_status::OK,
        Err(code) => code,
    }
}

/// `scene_insert_coverage` (rev 16, ticket 52): one clip-path
/// coverage entry (a path reference plus its device bounds). Returns
/// the assigned coverage index through `out_index` (null allowed).
fn scene_insert_coverage_body(
    handle: u64,
    rec: *const GpuiGoSceneCoverageRecord,
    out_index: *mut u32,
) -> i32 {
    if rec.is_null() {
        return scene_status::ERR_NULL_ARG;
    }
    let result = with_scene(handle, |state| {
        let record = unsafe { *rec };
        check_coverage(&record)?;
        let index = state.coverage.len() as u32;
        state.insert_coverage(record)?;
        if !out_index.is_null() {
            unsafe { *out_index = index };
        }
        Ok(())
    });
    match result {
        Ok(()) => scene_status::OK,
        Err(code) => code,
    }
}

/// `scene_insert_filter_op_chain` (rev 16, ticket 52): one drop-shadow
/// op chain (the design's appendix A.5). Returns the assigned chain
/// index through `out_index` (null allowed).
fn scene_insert_filter_op_chain_body(
    handle: u64,
    ops: *const GpuiGoSceneFilterOpRecord,
    op_count: u32,
    out_index: *mut u32,
) -> i32 {
    if op_count == 0 || ops.is_null() {
        return scene_status::ERR_NULL_ARG;
    }
    let op_slice: &[GpuiGoSceneFilterOpRecord] =
        unsafe { std::slice::from_raw_parts(ops, op_count as usize) };
    for op in op_slice {
        if let Err(code) = check_filter_op(op) {
            return code;
        }
    }
    let result = with_scene(handle, |state| {
        let index = state.filter_op_chains.len() as u32;
        state.insert_filter_op_chain(op_slice)?;
        if !out_index.is_null() {
            unsafe { *out_index = index };
        }
        Ok(())
    });
    match result {
        Ok(()) => scene_status::OK,
        Err(code) => code,
    }
}

/// `scene_update_property` (rev 16, ticket 51, the design's appendix
/// A.7): mutates every FINISHED effect record carrying `node_id` in
/// place - the new matrix or opacity, no re-sort, no re-plan (the
/// paint order never changes on a transform/opacity tick, the same
/// invariant cc relies on when ticking compositor animations) - and
/// reports the group's OLD and NEW device-space bounds (mapped
/// through the old/new matrix; the record bounds when the node has no
/// transform), the damage union the caller re-presents. The plan is
/// untouched: the render walk consults the CURRENT record (a
/// translation-only group the plan inlined for promotes back to an
/// isolated bucket at draw time when the update turns its matrix on).
///
/// Errors: `ERR_NOT_FINISHED` before `scene_finish`, `ERR_NULL_ARG`
/// for null values, `ERR_BAD_VALUE` for an unknown kind, a wrong
/// value count, a zero node id, a stale id (no record carries it), a
/// missing required record flag (`has_transform` for the matrix kind,
/// `has_opacity` for the opacity kind) or a value the insert
/// validation would reject (a non-finite or non-invertible matrix, an
/// opacity outside [0, 1]).
fn scene_update_property_body(
    handle: u64,
    node_id: u32,
    kind: u32,
    values: *const u32,
    value_count: u32,
    out_old: *mut GpuiGoSceneBounds,
    out_new: *mut GpuiGoSceneBounds,
) -> i32 {
    if values.is_null() {
        return scene_status::ERR_NULL_ARG;
    }
    if node_id == 0 {
        return scene_status::ERR_BAD_VALUE;
    }
    let expected_count = match kind {
        scene_property_kind::MATRIX => 6usize,
        scene_property_kind::OPACITY => 1usize,
        _ => return scene_status::ERR_BAD_VALUE,
    };
    if value_count as usize != expected_count {
        return scene_status::ERR_BAD_VALUE;
    }
    let mut words = [0u32; 6];
    unsafe {
        std::ptr::copy_nonoverlapping(values, words.as_mut_ptr(), expected_count);
    }
    // The value validation mirrors the insert rules (check_effect):
    // the matrix needs a finite, invertible 2x2 (and finite
    // translation, so the mapped bounds stay representable); the
    // opacity lies in [0, 1].
    match kind {
        scene_property_kind::MATRIX => {
            let matrix = [
                f32::from_bits(words[0]),
                f32::from_bits(words[1]),
                f32::from_bits(words[2]),
                f32::from_bits(words[3]),
                f32::from_bits(words[4]),
                f32::from_bits(words[5]),
            ];
            for value in matrix {
                if !value.is_finite() {
                    return scene_status::ERR_BAD_VALUE;
                }
            }
            let determinant = matrix[0] * matrix[3] - matrix[1] * matrix[2];
            if determinant == 0.0 || !determinant.is_finite() {
                return scene_status::ERR_BAD_VALUE;
            }
        }
        _ => {
            let opacity = f32::from_bits(words[0]);
            if !opacity.is_finite() || !(0.0..=1.0).contains(&opacity) {
                return scene_status::ERR_BAD_VALUE;
            }
        }
    }
    let result = with_scene(handle, |state| {
        if !state.is_finished {
            return Err(scene_status::ERR_NOT_FINISHED);
        }
        // The group's shared snapshot answers the bounds: the FIRST
        // record carrying the id (a well-formed pair inserts the start
        // marker first; every record of the pair carries the same
        // fields).
        let Some(first) = state.effects.iter().position(|rec| rec.node_id == node_id) else {
            return Err(scene_status::ERR_BAD_VALUE);
        };
        let record_bounds = [
            f32::from_bits(state.effects[first].bounds.x),
            f32::from_bits(state.effects[first].bounds.y),
            f32::from_bits(state.effects[first].bounds.w),
            f32::from_bits(state.effects[first].bounds.h),
        ];
        let mapped_bounds = |matrix: [f32; 6]| -> GpuiGoSceneBounds {
            let mapped = map_bounds_corners(record_bounds, matrix);
            GpuiGoSceneBounds {
                x: mapped[0].to_bits(),
                y: mapped[1].to_bits(),
                w: mapped[2].to_bits(),
                h: mapped[3].to_bits(),
            }
        };
        let raw_bounds = || GpuiGoSceneBounds {
            x: state.effects[first].bounds.x,
            y: state.effects[first].bounds.y,
            w: state.effects[first].bounds.w,
            h: state.effects[first].bounds.h,
        };
        let (old_bounds, new_bounds) = match kind {
            scene_property_kind::MATRIX => {
                if (state.effects[first].flags & effect_flags::HAS_TRANSFORM) == 0 {
                    return Err(scene_status::ERR_BAD_VALUE);
                }
                let old = mapped_bounds(effect_matrix(&state.effects[first]));
                let new = mapped_bounds([
                    f32::from_bits(words[0]),
                    f32::from_bits(words[1]),
                    f32::from_bits(words[2]),
                    f32::from_bits(words[3]),
                    f32::from_bits(words[4]),
                    f32::from_bits(words[5]),
                ]);
                for record in state.effects.iter_mut() {
                    if record.node_id != node_id {
                        continue;
                    }
                    if (record.flags & effect_flags::HAS_TRANSFORM) == 0 {
                        // The pair's end marker carries the same
                        // snapshot: a node whose records disagree is
                        // malformed; refuse rather than half-mutate.
                        return Err(scene_status::ERR_BAD_VALUE);
                    }
                    record.transform = words;
                }
                (old, new)
            }
            _ => {
                if (state.effects[first].flags & effect_flags::HAS_OPACITY) == 0 {
                    return Err(scene_status::ERR_BAD_VALUE);
                }
                // The opacity never moves the bounds: the mapped
                // device bounds of the (unchanged) transform.
                let bounds = if (state.effects[first].flags & effect_flags::HAS_TRANSFORM) != 0 {
                    mapped_bounds(effect_matrix(&state.effects[first]))
                } else {
                    raw_bounds()
                };
                for record in state.effects.iter_mut() {
                    if record.node_id != node_id {
                        continue;
                    }
                    if (record.flags & effect_flags::HAS_OPACITY) == 0 {
                        return Err(scene_status::ERR_BAD_VALUE);
                    }
                    record.opacity = words[0];
                }
                (bounds, bounds)
            }
        };
        if !out_old.is_null() {
            unsafe { *out_old = old_bounds };
        }
        if !out_new.is_null() {
            unsafe { *out_new = new_bounds };
        }
        Ok(())
    });
    match result {
        Ok(()) => scene_status::OK,
        Err(code) => code,
    }
}

fn scene_insert_surface_body(
    handle: u64,
    rec: *const GpuiGoSceneSurfaceRecord,
    opacity_bits: u32,
) -> i32 {
    if rec.is_null() {
        return scene_status::ERR_NULL_ARG;
    }
    let record: &GpuiGoSceneSurfaceRecord = unsafe { &*rec };
    if let Err(code) = check_surface(record) {
        return code;
    }
    let record = *record;
    let opacity = f32::from_bits(opacity_bits);
    match with_scene(handle, |state| {
        state.insert_surface(record, opacity)
    }) {
        Ok(()) => scene_status::OK,
        Err(code) => code,
    }
}

fn scene_insert_monochrome_sprite_body(
    handle: u64,
    rec: *const GpuiGoSceneSpriteRecord,
) -> i32 {
    if rec.is_null() {
        return scene_status::ERR_NULL_ARG;
    }
    let record: &GpuiGoSceneSpriteRecord = unsafe { &*rec };
    if let Err(code) = check_mask_sprite(record, SpriteClass::Monochrome) {
        return code;
    }
    let record = *record;
    match with_scene(handle, |state| {
        state.insert_mask_sprite(record, SpriteClass::Monochrome)
    }) {
        Ok(()) => scene_status::OK,
        Err(code) => code,
    }
}

fn scene_insert_subpixel_sprite_body(
    handle: u64,
    rec: *const GpuiGoSceneSpriteRecord,
) -> i32 {
    if rec.is_null() {
        return scene_status::ERR_NULL_ARG;
    }
    let record: &GpuiGoSceneSpriteRecord = unsafe { &*rec };
    if let Err(code) = check_mask_sprite(record, SpriteClass::Subpixel) {
        return code;
    }
    let record = *record;
    match with_scene(handle, |state| {
        state.insert_mask_sprite(record, SpriteClass::Subpixel)
    }) {
        Ok(()) => scene_status::OK,
        Err(code) => code,
    }
}

fn scene_insert_polychrome_sprite_body(
    handle: u64,
    rec: *const GpuiGoScenePolychromeSpriteRecord,
) -> i32 {
    if rec.is_null() {
        return scene_status::ERR_NULL_ARG;
    }
    let record: &GpuiGoScenePolychromeSpriteRecord = unsafe { &*rec };
    if let Err(code) = check_polychrome_sprite(record) {
        return code;
    }
    let record = *record;
    match with_scene(handle, |state| state.insert_polychrome_sprite(record)) {
        Ok(()) => scene_status::OK,
        Err(code) => code,
    }
}

/// Op kinds of the packed batch stream (`scene_insert_batch`, gpui-perf
/// ticket 05). Each op is a two-word header `[kind, payload_words]`
/// followed by `payload_words` u32 words: the exact `#[repr(C)]` record
/// of the matching single-op entry (all records are 4-byte aligned and a
/// whole number of words), plus one trailing opacity word for surfaces;
/// the four bounds words for a layer push; nothing for a pop or floor.
pub mod batch_op {
    /// A quad record.
    pub const QUAD: u32 = 0;
    /// A shadow record.
    pub const SHADOW: u32 = 1;
    /// An underline record.
    pub const UNDERLINE: u32 = 2;
    /// A backdrop-filter record.
    pub const BACKDROP: u32 = 3;
    /// A filter-boundary record.
    pub const BOUNDARY: u32 = 4;
    /// A surface record followed by its opacity bits.
    pub const SURFACE: u32 = 5;
    /// A monochrome sprite record.
    pub const MONOCHROME_SPRITE: u32 = 6;
    /// A subpixel sprite record.
    pub const SUBPIXEL_SPRITE: u32 = 7;
    /// A polychrome sprite record.
    pub const POLYCHROME_SPRITE: u32 = 8;
    /// A layer push (bounds record).
    pub const PUSH_LAYER: u32 = 9;
    /// A layer pop (no payload).
    pub const POP_LAYER: u32 = 10;
    /// An order-floor raise (no payload).
    pub const RAISE_ORDER_FLOOR: u32 = 11;
    /// `GpuiGoSceneEffectRecord` with `flags.IS_START` set (rev 16,
    /// ticket 50).
    pub const EFFECT_START: u32 = 12;
    /// `GpuiGoSceneEffectRecord` with `flags.IS_START` clear (rev 16,
    /// ticket 50).
    pub const EFFECT_END: u32 = 13;
    /// One `GpuiGoSceneGradientRecord` (56 bytes, 14 words) followed by
    /// its `stop_count` `GpuiGoSceneGradientStop` rows (2 words each;
    /// rev 16, ticket 52).
    pub const GRADIENT: u32 = 14;
    /// One `GpuiGoSceneCoverageRecord` (24 bytes, 6 words; rev 16,
    /// ticket 52).
    pub const COVERAGE: u32 = 15;
    /// A filter-op chain: one word of the op count followed by that many
    /// `GpuiGoSceneFilterOpRecord` rows (10 words each; rev 16, ticket
    /// 52).
    pub const FILTER_OP_CHAIN: u32 = 16;
}

/// Reads one record of type `T` from a word payload that must be exactly
/// `size_of::<T>()` bytes long.
fn batch_record<T: Copy>(payload: &[u32]) -> Result<T, i32> {
    if payload.len() * 4 != core::mem::size_of::<T>() {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    Ok(unsafe { core::ptr::read_unaligned(payload.as_ptr().cast::<T>()) })
}

/// Applies one batch op with exactly the validation and state transition
/// of the matching single-op entry.
fn apply_batch_op(state: &mut SceneState, kind: u32, payload: &[u32]) -> Result<(), i32> {
    match kind {
        batch_op::QUAD => {
            let rec: GpuiGoSceneQuadRecord = batch_record(payload)?;
            check_quad(&rec)?;
            state.insert_quad(rec)
        }
        batch_op::SHADOW => {
            let rec: GpuiGoSceneShadowRecord = batch_record(payload)?;
            check_shadow(&rec)?;
            state.insert_shadow(rec)
        }
        batch_op::UNDERLINE => {
            let rec: GpuiGoSceneUnderlineRecord = batch_record(payload)?;
            check_underline(&rec)?;
            state.insert_underline(rec)
        }
        batch_op::BACKDROP => {
            let rec: GpuiGoSceneFilterRecord = batch_record(payload)?;
            check_filter(&rec)?;
            state.insert_backdrop(rec)
        }
        batch_op::BOUNDARY => {
            let rec: GpuiGoSceneFilterRecord = batch_record(payload)?;
            check_filter(&rec)?;
            state.insert_boundary(rec)
        }
        batch_op::EFFECT_START | batch_op::EFFECT_END => {
            let mut record: GpuiGoSceneEffectRecord = batch_record(payload)?;
            if kind == batch_op::EFFECT_START {
                record.flags |= effect_flags::IS_START;
            } else {
                record.flags &= !effect_flags::IS_START;
            }
            check_effect(&record)?;
            state.insert_effect(record)
        }
        batch_op::GRADIENT => {
            // The record (14 words) followed by the stop rows (2 words
            // each, `record.stop_count` of them).
            const RECORD_WORDS: usize =
                core::mem::size_of::<GpuiGoSceneGradientRecord>() / 4;
            if payload.len() < RECORD_WORDS {
                return Err(scene_status::ERR_BAD_VALUE);
            }
            let (record_words, stop_words) = payload.split_at(RECORD_WORDS);
            let rec: GpuiGoSceneGradientRecord = batch_record(record_words)?;
            if stop_words.len() % 2 != 0
                || (rec.stop_count as usize) * 2 != stop_words.len()
            {
                return Err(scene_status::ERR_BAD_VALUE);
            }
            let stops: Vec<GpuiGoSceneGradientStop> = stop_words
                .chunks_exact(2)
                .map(|pair| GpuiGoSceneGradientStop { position: pair[0], rgba: pair[1] })
                .collect();
            check_gradient(&rec, &stops)?;
            state.insert_gradient(rec, &stops)
        }
        batch_op::COVERAGE => {
            let rec: GpuiGoSceneCoverageRecord = batch_record(payload)?;
            check_coverage(&rec)?;
            state.insert_coverage(rec)
        }
        batch_op::FILTER_OP_CHAIN => {
            const OP_WORDS: usize = core::mem::size_of::<GpuiGoSceneFilterOpRecord>() / 4;
            let Some((&count, rest)) = payload.split_first() else {
                return Err(scene_status::ERR_BAD_VALUE);
            };
            if count == 0
                || count > MAX_SCENE_FILTER_OPS
                || rest.len() != count as usize * OP_WORDS
            {
                return Err(scene_status::ERR_BAD_VALUE);
            }
            let ops: Vec<GpuiGoSceneFilterOpRecord> = rest
                .chunks_exact(OP_WORDS)
                .map(|words| {
                    unsafe { core::ptr::read_unaligned(words.as_ptr().cast::<GpuiGoSceneFilterOpRecord>()) }
                })
                .collect();
            for op in ops.iter() {
                check_filter_op(op)?;
            }
            state.insert_filter_op_chain(&ops)
        }
        batch_op::SURFACE => {
            let Some((&opacity_bits, record)) = payload.split_last() else {
                return Err(scene_status::ERR_BAD_VALUE);
            };
            let rec: GpuiGoSceneSurfaceRecord = batch_record(record)?;
            check_surface(&rec)?;
            state.insert_surface(rec, f32::from_bits(opacity_bits))
        }
        batch_op::MONOCHROME_SPRITE => {
            let rec: GpuiGoSceneSpriteRecord = batch_record(payload)?;
            check_mask_sprite(&rec, SpriteClass::Monochrome)?;
            state.insert_mask_sprite(rec, SpriteClass::Monochrome)
        }
        batch_op::SUBPIXEL_SPRITE => {
            let rec: GpuiGoSceneSpriteRecord = batch_record(payload)?;
            check_mask_sprite(&rec, SpriteClass::Subpixel)?;
            state.insert_mask_sprite(rec, SpriteClass::Subpixel)
        }
        batch_op::POLYCHROME_SPRITE => {
            let rec: GpuiGoScenePolychromeSpriteRecord = batch_record(payload)?;
            check_polychrome_sprite(&rec)?;
            state.insert_polychrome_sprite(rec)
        }
        batch_op::PUSH_LAYER => {
            let bounds: GpuiGoSceneBounds = batch_record(payload)?;
            state.push_layer(bounds.to_f32())
        }
        batch_op::POP_LAYER if payload.is_empty() => state.pop_layer(),
        batch_op::RAISE_ORDER_FLOOR if payload.is_empty() => state.raise_order_floor(),
        _ => Err(scene_status::ERR_BAD_VALUE),
    }
}

/// The batch body (gpui-perf ticket 05): applies a packed op stream in
/// order under one scene lock — the same sequence of state transitions
/// as one single-op call per op, so orders, the op log and the plan are
/// identical. Stops at the first failing op; `out_applied` receives the
/// number of ops applied before it (all of them on success). A malformed
/// header (truncated payload) fails with ERR_BAD_VALUE at that op.
fn scene_insert_batch_body(
    handle: u64,
    words: *const u32,
    word_count: u32,
    out_applied: *mut u32,
) -> i32 {
    if out_applied.is_null() || (word_count > 0 && words.is_null()) {
        return scene_status::ERR_NULL_ARG;
    }
    unsafe { *out_applied = 0 };
    let stream: &[u32] = if word_count == 0 {
        &[]
    } else {
        unsafe { std::slice::from_raw_parts(words, word_count as usize) }
    };
    let mut applied = 0u32;
    let result = with_scene(handle, |state| {
        let mut rest = stream;
        while !rest.is_empty() {
            if rest.len() < 2 {
                return Err(scene_status::ERR_BAD_VALUE);
            }
            let kind = rest[0];
            let len = rest[1] as usize;
            if rest.len() - 2 < len {
                return Err(scene_status::ERR_BAD_VALUE);
            }
            apply_batch_op(state, kind, &rest[2..2 + len])?;
            applied += 1;
            rest = &rest[2 + len..];
        }
        Ok(())
    });
    unsafe { *out_applied = applied };
    match result {
        Ok(()) => scene_status::OK,
        Err(code) => code,
    }
}

fn scene_replay_body(handle: u64, start: u32, end: u32, prev: u64) -> i32 {
    if prev == handle {
        // The pinned signature borrows the source immutably and the
        // target mutably; a scene cannot replay itself.
        return scene_status::ERR_BAD_VALUE;
    }
    // Copy the source's operations out under its own lock, then insert
    // into the target (no two locks held at once).
    let prev_ops = {
        let shared = match resolve_scene(prev) {
            Ok(shared) => shared,
            Err(code) => return code,
        };
        if shared.busy.swap(true, Ordering::SeqCst) {
            return scene_status::ERR_BAD_VALUE;
        }
        let _busy_guard = BusyGuard(shared.clone());
        let state = lock_state(&shared);
        if state.failed {
            return scene_status::ERR_SCENE_FAILED;
        }
        let start = start as usize;
        let end = end as usize;
        if start > end || end > state.ops.len() {
            return scene_status::ERR_BAD_VALUE;
        }
        state.ops[start..end].to_vec()
    };
    match with_scene(handle, |state| {
        state.replay(start as usize, end as usize, prev_ops)
    }) {
        Ok(()) => scene_status::OK,
        Err(code) => code,
    }
}

fn scene_finish_body(handle: u64) -> i32 {
    match with_scene(handle, |state| state.finish()) {
        Ok(()) => scene_status::OK,
        Err(code) => code,
    }
}

fn scene_len_body(handle: u64, out_len: *mut u32) -> i32 {
    if out_len.is_null() {
        return scene_status::ERR_NULL_ARG;
    }
    match with_scene(handle, |state| Ok(state.ops.len() as u32)) {
        Ok(len) => {
            unsafe { *out_len = len };
            scene_status::OK
        }
        Err(code) => code,
    }
}

fn scene_meta_body(handle: u64, out_meta: *mut GpuiGoSceneMetaRecord) -> i32 {
    if out_meta.is_null() {
        return scene_status::ERR_NULL_ARG;
    }
    let result = with_scene(handle, |state| {
        Ok(GpuiGoSceneMetaRecord {
            op_count: state.ops.len() as u32,
            layer_push_count: state.layer_push_count,
            quad_count: state.quads.len() as u32,
            shadow_count: state.shadows.len() as u32,
            underline_count: state.underlines.len() as u32,
            backdrop_count: state.backdrops.len() as u32,
            boundary_count: state.boundaries.len() as u32,
            surface_count: state.surfaces.len() as u32,
            monochrome_sprite_count: state.monochrome_sprites.len() as u32,
            subpixel_sprite_count: state.subpixel_sprites.len() as u32,
            polychrome_sprite_count: state.polychrome_sprites.len() as u32,
            path_count: state.paths.len() as u32,
            effect_count: state.effects.len() as u32,
            gradient_count: state.gradients.len() as u32,
            coverage_count: state.coverage.len() as u32,
            filter_chain_count: state.filter_op_chains.len() as u32,
            is_finished: u32::from(state.is_finished),
            record_size: core::mem::size_of::<GpuiGoSceneMetaRecord>() as u32,
        })
    });
    match result {
        Ok(meta) => {
            unsafe { *out_meta = meta };
            scene_status::OK
        }
        Err(code) => code,
    }
}

/// Dump kinds of `scene_dump`.
mod dump_kind {
    pub const QUADS: u32 = 0;
    pub const SHADOWS: u32 = 1;
    pub const UNDERLINES: u32 = 2;
    pub const BACKDROPS: u32 = 3;
    pub const BOUNDARIES: u32 = 4;
    pub const SURFACES: u32 = 5;
    pub const MONOCHROME_SPRITES: u32 = 6;
    pub const SUBPIXEL_SPRITES: u32 = 7;
    pub const POLYCHROME_SPRITES: u32 = 8;
}

fn scene_dump_body(
    handle: u64,
    kind: u32,
    records: *mut u8,
    opacity_bits: *mut u32,
    capacity: u32,
    out_count: *mut u32,
) -> i32 {
    if out_count.is_null() {
        return scene_status::ERR_NULL_ARG;
    }
    let result = with_scene(handle, |state| {
        if !state.is_finished {
            return Err(scene_status::ERR_NOT_FINISHED);
        }
        let (len, record_size): (usize, usize) = match kind {
            dump_kind::QUADS => (
                state.quads.len(),
                core::mem::size_of::<GpuiGoSceneQuadRecord>(),
            ),
            dump_kind::SHADOWS => (
                state.shadows.len(),
                core::mem::size_of::<GpuiGoSceneShadowRecord>(),
            ),
            dump_kind::UNDERLINES => (
                state.underlines.len(),
                core::mem::size_of::<GpuiGoSceneUnderlineRecord>(),
            ),
            dump_kind::BACKDROPS => (
                state.backdrops.len(),
                core::mem::size_of::<GpuiGoSceneFilterRecord>(),
            ),
            dump_kind::BOUNDARIES => (
                state.boundaries.len(),
                core::mem::size_of::<GpuiGoSceneFilterRecord>(),
            ),
            dump_kind::SURFACES => (
                state.surfaces.len(),
                core::mem::size_of::<GpuiGoSceneSurfaceRecord>(),
            ),
            dump_kind::MONOCHROME_SPRITES => (
                state.monochrome_sprites.len(),
                core::mem::size_of::<GpuiGoSceneSpriteRecord>(),
            ),
            dump_kind::SUBPIXEL_SPRITES => (
                state.subpixel_sprites.len(),
                core::mem::size_of::<GpuiGoSceneSpriteRecord>(),
            ),
            dump_kind::POLYCHROME_SPRITES => (
                state.polychrome_sprites.len(),
                core::mem::size_of::<GpuiGoScenePolychromeSpriteRecord>(),
            ),
            _ => return Err(scene_status::ERR_BAD_VALUE),
        };
        let needed = len as u32;
        // Report the required count (always) before judging capacity.
        unsafe { *out_count = needed };
        if needed > capacity {
            return Err(scene_status::ERR_CAPACITY);
        }
        if capacity > 0 && records.is_null() {
            return Err(scene_status::ERR_NULL_ARG);
        }
        if kind == dump_kind::SURFACES && capacity > 0 && opacity_bits.is_null() {
            return Err(scene_status::ERR_NULL_ARG);
        }
        // Copy the packed records into the caller buffer. The buffers are
        // caller-owned and exclusively borrowed for this call; the copy
        // happens under the state lock so the dump is atomic against
        // other calls on this scene.
        let copy = |source: &[u8]| unsafe {
            let out = std::slice::from_raw_parts_mut(records, source.len());
            out.copy_from_slice(source);
        };
        match kind {
            dump_kind::QUADS => copy(unsafe {
                std::slice::from_raw_parts(
                    state.quads.as_ptr() as *const u8,
                    state.quads.len() * record_size,
                )
            }),
            dump_kind::SHADOWS => copy(unsafe {
                std::slice::from_raw_parts(
                    state.shadows.as_ptr() as *const u8,
                    state.shadows.len() * record_size,
                )
            }),
            dump_kind::UNDERLINES => copy(unsafe {
                std::slice::from_raw_parts(
                    state.underlines.as_ptr() as *const u8,
                    state.underlines.len() * record_size,
                )
            }),
            dump_kind::BACKDROPS => copy(unsafe {
                std::slice::from_raw_parts(
                    state.backdrops.as_ptr() as *const u8,
                    state.backdrops.len() * record_size,
                )
            }),
            dump_kind::BOUNDARIES => copy(unsafe {
                std::slice::from_raw_parts(
                    state.boundaries.as_ptr() as *const u8,
                    state.boundaries.len() * record_size,
                )
            }),
            dump_kind::SURFACES => {
                copy(unsafe {
                    std::slice::from_raw_parts(
                        state.surfaces.as_ptr() as *const u8,
                        state.surfaces.len() * record_size,
                    )
                });
                if !opacity_bits.is_null() {
                    let opacities =
                        unsafe { std::slice::from_raw_parts_mut(opacity_bits, capacity as usize) };
                    for (slot, opacity) in
                        opacities.iter_mut().zip(state.surface_opacities.iter())
                    {
                        *slot = opacity.to_bits();
                    }
                }
            }
            dump_kind::MONOCHROME_SPRITES => copy(unsafe {
                std::slice::from_raw_parts(
                    state.monochrome_sprites.as_ptr() as *const u8,
                    state.monochrome_sprites.len() * record_size,
                )
            }),
            dump_kind::SUBPIXEL_SPRITES => copy(unsafe {
                std::slice::from_raw_parts(
                    state.subpixel_sprites.as_ptr() as *const u8,
                    state.subpixel_sprites.len() * record_size,
                )
            }),
            dump_kind::POLYCHROME_SPRITES => copy(unsafe {
                std::slice::from_raw_parts(
                    state.polychrome_sprites.as_ptr() as *const u8,
                    state.polychrome_sprites.len() * record_size,
                )
            }),
            _ => return Err(scene_status::ERR_BAD_VALUE),
        }
        Ok(())
    });
    match result {
        Ok(()) => scene_status::OK,
        Err(code) => code,
    }
}

fn scene_plan_dump_body(
    handle: u64,
    commands: *mut GpuiGoSceneCommandRecord,
    capacity: u32,
    out_count: *mut u32,
) -> i32 {
    if out_count.is_null() {
        return scene_status::ERR_NULL_ARG;
    }
    let result = with_scene(handle, |state| {
        if !state.is_finished {
            return Err(scene_status::ERR_NOT_FINISHED);
        }
        let needed = state.plan.commands.len() as u32;
        unsafe { *out_count = needed };
        if needed > capacity {
            return Err(scene_status::ERR_CAPACITY);
        }
        if capacity > 0 && commands.is_null() {
            return Err(scene_status::ERR_NULL_ARG);
        }
        if capacity > 0 {
            let out = unsafe { std::slice::from_raw_parts_mut(commands, capacity as usize) };
            for (slot, command) in out.iter_mut().zip(state.plan.commands.iter()) {
                *slot = *command;
            }
        }
        Ok(())
    });
    match result {
        Ok(()) => scene_status::OK,
        Err(code) => code,
    }
}

fn scene_requirements_body(handle: u64, out: *mut GpuiGoSceneRequirementsRecord) -> i32 {
    if out.is_null() {
        return scene_status::ERR_NULL_ARG;
    }
    let result = with_scene(handle, |state| {
        if !state.is_finished {
            return Err(scene_status::ERR_NOT_FINISHED);
        }
        Ok(state.plan.requirements)
    });
    match result {
        Ok(mut requirements) => {
            requirements.record_size =
                core::mem::size_of::<GpuiGoSceneRequirementsRecord>() as u32;
            unsafe { *out = requirements };
            scene_status::OK
        }
        Err(code) => code,
    }
}

fn scene_panic_probe_body() -> i32 {
    PANIC_PROBE_INVOCATIONS.fetch_add(1, Ordering::Relaxed);
    panic!("gpui-go scene panic probe (contained by design)");
}

// ---------------------------------------------------------------------------
// Path insertion, dump and the pinned PathBuilder tessellation (ticket16)
// ---------------------------------------------------------------------------

/// Validates a path record: record size, zero order, solid color tag,
/// vertex count bound and count agreement with the bulk array.
fn check_path(rec: &GpuiGoScenePathRecord, vertex_count: u32) -> Result<(), i32> {
    check_record_size(rec.record_size, core::mem::size_of::<GpuiGoScenePathRecord>() as u32)?;
    if rec.order != 0 {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    // The pinned path-sprite pipeline cannot evaluate the R16
    // gradient-table arm (rev 16, ticket 52): the table tag is
    // rejected on path fills.
    check_background_with(&rec.color, false)?;
    if rec.vertex_count != vertex_count {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    if vertex_count > MAX_SCENE_PATH_VERTICES {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    check_content_mask(&rec.content_mask)?;
    Ok(())
}

/// Validates one path-builder command: record size, kind tag, finite
/// floats, enum tag ranges, boolean tags and word-pool indices. `words`
/// is the call's shared word pool (point coordinates and dash lengths).
fn check_path_command(cmd: &GpuiGoPathCommandRecord, words: &[u32]) -> Result<(), i32> {
    check_record_size(cmd.record_size, core::mem::size_of::<GpuiGoPathCommandRecord>() as u32)?;
    let data = &cmd.data;
    let word = |index: usize| f32::from_bits(data[index]);
    let finite = |index: usize| -> Result<(), i32> {
        if !word(index).is_finite() {
            return Err(scene_status::ERR_BAD_VALUE);
        }
        Ok(())
    };
    let zeros = |indices: &[usize]| -> Result<(), i32> {
        for index in indices {
            if data[*index] != 0 {
                return Err(scene_status::ERR_BAD_VALUE);
            }
        }
        Ok(())
    };
    let pool_range = |first: u32, count: u32| -> Result<(), i32> {
        let end = (first as u64) + (count as u64);
        if end > words.len() as u64 {
            return Err(scene_status::ERR_BAD_VALUE);
        }
        Ok(())
    };
    match cmd.kind {
        path_command_kind::MOVE_TO | path_command_kind::LINE_TO | path_command_kind::TRANSLATE => {
            finite(0)?;
            finite(1)?;
            zeros(&[2, 3, 4, 5, 6, 7, 8, 9, 10, 11])?;
        }
        path_command_kind::CURVE_TO => {
            finite(0)?;
            finite(1)?;
            finite(2)?;
            finite(3)?;
            zeros(&[4, 5, 6, 7, 8, 9, 10, 11])?;
        }
        path_command_kind::CUBIC_BEZIER_TO => {
            for index in 0..6 {
                finite(index)?;
            }
            zeros(&[6, 7, 8, 9, 10, 11])?;
        }
        path_command_kind::ARC_TO | path_command_kind::RELATIVE_ARC_TO => {
            finite(0)?;
            finite(1)?;
            finite(2)?;
            check_bool01(data[3])?;
            check_bool01(data[4])?;
            finite(5)?;
            finite(6)?;
            zeros(&[7, 8, 9, 10, 11])?;
        }
        path_command_kind::POLYGON => {
            pool_range(data[0], data[1].checked_mul(2).ok_or(scene_status::ERR_BAD_VALUE)?)?;
            check_bool01(data[2])?;
            zeros(&[3, 4, 5, 6, 7, 8, 9, 10, 11])?;
        }
        path_command_kind::CLOSE => {
            zeros(&[0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11])?;
        }        path_command_kind::STYLE => {
            if data[0] == path_style_tag::FILL {
                finite(1)?;
                if data[2] > 1 {
                    return Err(scene_status::ERR_BAD_VALUE);
                }
                if data[3] > 1 {
                    return Err(scene_status::ERR_BAD_VALUE);
                }
                check_bool01(data[4])?;
                zeros(&[5, 6, 7, 8, 9, 10, 11])?;
            } else if data[0] == path_style_tag::STROKE {
                finite(1)?;
                if data[2] > 2 || data[3] > 2 || data[4] > 2 {
                    return Err(scene_status::ERR_BAD_VALUE);
                }
                finite(5)?;
                zeros(&[6, 7, 8, 9, 10, 11])?;
            } else {
                return Err(scene_status::ERR_BAD_VALUE);
            }
        }
        path_command_kind::DASH => {
            pool_range(data[0], data[1])?;
            zeros(&[2, 3, 4, 5, 6, 7, 8, 9, 10, 11])?;
        }
        path_command_kind::SCALE | path_command_kind::ROTATE => {
            finite(0)?;
            zeros(&[1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11])?;
        }
        path_command_kind::TRANSFORM => {
            for index in 0..6 {
                finite(index)?;
            }
            zeros(&[6, 7, 8, 9, 10, 11])?;
        }
        _ => return Err(scene_status::ERR_BAD_VALUE),
    }
    Ok(())
}

/// The path-insertion body: validate the record and the bulk vertex
/// array, then hand both to the kernel's `insert_path` (the pinned
/// `Scene::insert_primitive(Primitive::Path(path))`).
fn scene_insert_path_body(
    handle: u64,
    rec: *const GpuiGoScenePathRecord,
    vertices: *const GpuiGoScenePathVertexRecord,
    vertex_count: u32,
) -> i32 {
    if rec.is_null() {
        return scene_status::ERR_NULL_ARG;
    }
    if vertex_count > 0 && vertices.is_null() {
        return scene_status::ERR_NULL_ARG;
    }
    let rec = unsafe { *rec };
    if let Err(code) = check_path(&rec, vertex_count) {
        return code;
    }
    let vertices: Vec<GpuiGoScenePathVertexRecord> = if vertex_count > 0 {
        unsafe { std::slice::from_raw_parts(vertices, vertex_count as usize).to_vec() }
    } else {
        Vec::new()
    };
    let result = with_scene(handle, |state| state.insert_path(rec, vertices));
    match result {
        Ok(()) => scene_status::OK,
        Err(code) => code,
    }
}

/// The path-dump body: bulk copy of the finished scene's path records
/// (with the assigned draw orders and per-path vertex counts) plus the
/// concatenated vertex array.
fn scene_path_dump_body(
    handle: u64,
    records: *mut GpuiGoScenePathRecord,
    vertices: *mut GpuiGoScenePathVertexRecord,
    record_capacity: u32,
    vertex_capacity: u32,
    out_record_count: *mut u32,
    out_vertex_count: *mut u32,
) -> i32 {
    if out_record_count.is_null() || out_vertex_count.is_null() {
        return scene_status::ERR_NULL_ARG;
    }
    let result = with_scene(handle, |state| {
        if !state.is_finished {
            return Err(scene_status::ERR_NOT_FINISHED);
        }
        let needed_records = state.paths.len() as u32;
        let needed_vertices: u32 = state
            .paths
            .iter()
            .map(|data| data.vertices.len() as u32)
            .sum();
        unsafe {
            *out_record_count = needed_records;
            *out_vertex_count = needed_vertices;
        }
        if needed_records > record_capacity || needed_vertices > vertex_capacity {
            return Err(scene_status::ERR_CAPACITY);
        }
        if record_capacity > 0 && records.is_null() {
            return Err(scene_status::ERR_NULL_ARG);
        }
        if vertex_capacity > 0 && vertices.is_null() {
            return Err(scene_status::ERR_NULL_ARG);
        }
        if record_capacity > 0 {
            let out = unsafe {
                std::slice::from_raw_parts_mut(records, record_capacity as usize)
            };
            for (slot, data) in out.iter_mut().zip(state.paths.iter()) {
                *slot = data.rec;
            }
        }
        if vertex_capacity > 0 {
            let out = unsafe {
                std::slice::from_raw_parts_mut(vertices, vertex_capacity as usize)
            };
            let mut cursor = 0usize;
            for data in &state.paths {
                out[cursor..cursor + data.vertices.len()]
                    .copy_from_slice(&data.vertices);
                cursor += data.vertices.len();
            }
        }
        Ok(())
    });
    match result {
        Ok(()) => scene_status::OK,
        Err(code) => code,
    }
}

/// Replays one validated path command onto the pinned `PathBuilder`
/// (each arm is the pinned public method; `words` is the shared word
/// pool).
fn apply_path_command(builder: &mut PathBuilder, cmd: &GpuiGoPathCommandRecord, words: &[u32]) {
    let data = &cmd.data;
    let f = |index: usize| f32::from_bits(data[index]);
    let pt = |index: usize| point(px(f(index)), px(f(index + 1)));
    let pool_pt = |index: usize| -> gpui::Point<Pixels> {
        point(
            px(f32::from_bits(words[index])),
            px(f32::from_bits(words[index + 1])),
        )
    };
    match cmd.kind {
        path_command_kind::MOVE_TO => builder.move_to(pt(0)),
        path_command_kind::LINE_TO => builder.line_to(pt(0)),
        path_command_kind::CURVE_TO => {
            // The pinned signature: curve_to(to, ctrl) (a quadratic
            // Bézier).
            builder.curve_to(pt(0), pt(2));
        }
        path_command_kind::CUBIC_BEZIER_TO => {
            builder.cubic_bezier_to(pt(0), pt(2), pt(4));
        }
        path_command_kind::ARC_TO => {
            builder.arc_to(
                pt(0),
                px(f(2)),
                data[3] != 0,
                data[4] != 0,
                pt(5),
            );
        }
        path_command_kind::RELATIVE_ARC_TO => {
            builder.relative_arc_to(
                pt(0),
                px(f(2)),
                data[3] != 0,
                data[4] != 0,
                pt(5),
            );
        }
        path_command_kind::POLYGON => {
            let first = data[0] as usize;
            let count = data[1] as usize;
            let points: Vec<gpui::Point<Pixels>> = (0..count)
                .map(|i| pool_pt(first + i * 2))
                .collect();
            builder.add_polygon(&points, data[2] != 0);
        }
        path_command_kind::CLOSE => builder.close(),
        path_command_kind::TRANSLATE => builder.translate(pt(0)),
        path_command_kind::SCALE => builder.scale(f(0)),
        path_command_kind::ROTATE => builder.rotate(f(0)),
        path_command_kind::TRANSFORM => {
            // The ABI matrix is row-major 2x2 then translation:
            // [m00, m01, m10, m11, tx, ty]. euclid's
            // `Transform::new(a, b, c, d, e, f)` is column-major
            // (columns (a, b), (c, d), translation (e, f)), so the
            // row-major matrix maps to new(m00, m10, m01, m11, tx, ty).
            builder.transform(LyonTransform::new(
                f(0),
                f(2),
                f(1),
                f(3),
                f(4),
                f(5),
            ));
        }
        path_command_kind::STYLE | path_command_kind::DASH => {
            // Setters are applied at build time (see scene_path_script_body);
            // they do not mutate the builder's raw path here.
        }
        _ => unreachable!("validated command kinds only"),
    }
}

/// The path-style setter of a STYLE command (the pinned `with_style`).
fn path_style_of(cmd: &GpuiGoPathCommandRecord) -> PathStyle {
    let data = &cmd.data;
    let f = |index: usize| f32::from_bits(data[index]);
    if data[0] == path_style_tag::FILL {
        let mut options = FillOptions::default();
        options.tolerance = f(1);
        options.fill_rule = if data[2] == 0 {
            FillRule::EvenOdd
        } else {
            FillRule::NonZero
        };
        options.sweep_orientation = if data[3] == 0 {
            lyon::tessellation::Orientation::Vertical
        } else {
            lyon::tessellation::Orientation::Horizontal
        };
        options.handle_intersections = data[4] != 0;
        PathStyle::Fill(options)
    } else {
        let mut options = StrokeOptions::default();
        options.line_width = f(1);
        options.start_cap = line_cap_of(data[2]);
        options.end_cap = line_cap_of(data[3]);
        options.line_join = if data[4] == 0 {
            LineJoin::Miter
        } else if data[4] == 1 {
            LineJoin::Round
        } else {
            LineJoin::Bevel
        };
        options.miter_limit = f(5);
        PathStyle::Stroke(options)
    }
}

fn line_cap_of(tag: u32) -> LineCap {
    match tag {
        0 => LineCap::Butt,
        1 => LineCap::Square,
        _ => LineCap::Round,
    }
}

/// The pinned-PathBuilder tessellation entry: replay a validated
/// command script onto the pinned `gpui::PathBuilder` (the same lyon
/// code the reference oracle uses), build the tessellated
/// `Path<Pixels>`, and return its vertices (logical pixels) and bounds
/// (the union of the triangles; `(0, 0, 0, 0)` for an empty
/// tessellation, mirroring the pinned `build_path` fallback).
///
/// The style and dash-array setters are applied before `build()` exactly
/// like the pinned builder chain (`with_style`/`dash_array`); a build
/// failure (lyon error) maps to [`scene_status::ERR_BAD_VALUE`].
fn scene_path_script_body(
    commands: *const GpuiGoPathCommandRecord,
    command_count: u32,
    points: *const u32,
    word_count: u32,
    out_vertices: *mut GpuiGoScenePathVertexRecord,
    vertex_capacity: u32,
    out_vertex_count: *mut u32,
    out_bounds: *mut GpuiGoSceneBounds,
) -> i32 {
    if out_vertex_count.is_null() || out_bounds.is_null() {
        return scene_status::ERR_NULL_ARG;
    }
    if command_count > 0 && commands.is_null() {
        return scene_status::ERR_NULL_ARG;
    }
    if word_count > 0 && points.is_null() {
        return scene_status::ERR_NULL_ARG;
    }
    if vertex_capacity > 0 && out_vertices.is_null() {
        return scene_status::ERR_NULL_ARG;
    }
    // Build the borrowed slices only for non-null pointers with nonzero
    // counts (`from_raw_parts` requires non-null even for length 0).
    let commands: &[GpuiGoPathCommandRecord] = if command_count > 0 {
        unsafe { std::slice::from_raw_parts(commands, command_count as usize) }
    } else {
        &[]
    };
    let words: &[u32] = if word_count > 0 {
        unsafe { std::slice::from_raw_parts(points, word_count as usize) }
    } else {
        &[]
    };
    for cmd in commands {
        if let Err(code) = check_path_command(cmd, words) {
            return code;
        }
    }

    let mut builder = PathBuilder::default();
    let mut style: Option<PathStyle> = None;
    let mut dash_lengths: Option<Vec<Pixels>> = None;
    for cmd in commands {
        match cmd.kind {
            path_command_kind::STYLE => style = Some(path_style_of(cmd)),
            path_command_kind::DASH => {
                let first = cmd.data[0] as usize;
                let count = cmd.data[1] as usize;
                dash_lengths = Some(
                    words[first..first + count]
                        .iter()
                        .map(|bits| px(f32::from_bits(*bits)))
                        .collect(),
                );
            }
            _ => apply_path_command(&mut builder, cmd, words),
        }
    }
    if let Some(style) = style {
        builder = builder.with_style(style);
    }
    if let Some(lengths) = dash_lengths {
        builder = builder.dash_array(&lengths);
    }

    let path: GpuiPath<Pixels> = match builder.build() {
        Ok(path) => path,
        Err(_) => return scene_status::ERR_BAD_VALUE,
    };

    let vertices: Vec<GpuiGoScenePathVertexRecord> = path
        .vertices
        .iter()
        .map(|vertex| GpuiGoScenePathVertexRecord {
            xy_x: f32::from(vertex.xy_position.x).to_bits(),
            xy_y: f32::from(vertex.xy_position.y).to_bits(),
            st_u: vertex.st_position.x.to_bits(),
            st_v: vertex.st_position.y.to_bits(),
        })
        .collect();
    let bounds = GpuiGoSceneBounds {
        x: f32::from(path.bounds.origin.x).to_bits(),
        y: f32::from(path.bounds.origin.y).to_bits(),
        w: f32::from(path.bounds.size.width).to_bits(),
        h: f32::from(path.bounds.size.height).to_bits(),
    };

    let needed = vertices.len() as u32;
    unsafe {
        *out_vertex_count = needed;
        *out_bounds = bounds;
    }
    if needed > vertex_capacity {
        return scene_status::ERR_CAPACITY;
    }
    if needed > 0 {
        let out =
            unsafe { std::slice::from_raw_parts_mut(out_vertices, needed as usize) };
        out.copy_from_slice(&vertices);
    }
    scene_status::OK
}

// ---------------------------------------------------------------------------
// The render snapshot (cross-service: the renderer service draws from this)
// ---------------------------------------------------------------------------

/// One finished scene's draw data in the PINNED primitive layouts (the
/// `#[repr(C)]` GPUI-CE scene structs whose layouts the generated
/// shaders were compiled against — the same bytes the pinned renderer
/// uploads into its whole-frame instance buffers). The renderer service
/// resolves a scene handle through this snapshot; the scene stays pinned
/// by the caller exactly like the pin's `&Scene` borrow.
pub(crate) struct SceneRenderSnapshot {
    /// The ABI quad records (rev 16, ticket 50: the vendored quad
    /// pipelines' instance layout IS this record - bounds, the extended
    /// content mask, backgrounds, radii, borders - so the draw path
    /// uploads them directly; the plan's single-primitive opacity folds
    /// are applied to these copies at snapshot time).
    pub quads: Vec<GpuiGoSceneQuadRecord>,
    /// The ABI shadow records (same rule as the quads).
    pub shadows: Vec<GpuiGoSceneShadowRecord>,
    pub underlines: Vec<gpui::Underline>,
    pub monochrome_sprites: Vec<gpui::MonochromeSprite>,
    pub subpixel_sprites: Vec<gpui::SubpixelSprite>,
    pub polychrome_sprites: Vec<gpui::PolychromeSprite>,
    pub paths: Vec<SnapshotPath>,
    /// The ABI surface records (source tag 0 = the pinned
    /// `SurfaceSource::Unsupported` stand-in; tag 1 = a capture frame
    /// handle in `source_reserved[0]`, the pinned
    /// `SurfaceSource::WindowsCapture` import — ticket28).
    pub surfaces: Vec<GpuiGoSceneSurfaceRecord>,
    /// The paired surface opacities (the pin's parallel
    /// `surface_opacities` array, carried through the finish sort).
    pub surface_opacities: Vec<f32>,
    /// The ABI backdrop-filter records (rev 15: they carry the
    /// colour-matrix payloads the pinned `BackdropFilter` cannot; the
    /// draw path reads bounds, radii, opacity, the blur radii and the
    /// matrix from them directly).
    pub backdrops: Vec<GpuiGoSceneFilterRecord>,
    /// The ABI filter-boundary records (same shape as the backdrops).
    pub boundaries: Vec<GpuiGoSceneFilterRecord>,
    /// The ABI effect-group records (rev 16, ticket 50; the draw path
    /// reads the bounds, mask, radii, smoothing and opacity from them
    /// directly - the composite pass's own record).
    pub effects: Vec<GpuiGoSceneEffectRecord>,
    /// The ABI gradient table records (rev 16, ticket 52): one per
    /// scene gradient, `stop_row` == the index (the renderer uploads
    /// them as the gradient instance buffer).
    pub gradients: Vec<GpuiGoSceneGradientRecord>,
    /// The baked premultiplied-sRGB ramp rows, concatenated (each row
    /// 1024x4 bytes, row i belonging to gradient i; rev 16, ticket
    /// 52).
    pub gradient_ramps: Vec<u8>,
    /// The ABI coverage-mask entries (rev 16, ticket 52: the clip-path
    /// sources the effect groups reference).
    pub coverage: Vec<GpuiGoSceneCoverageRecord>,
    /// The ABI filter-op chains (rev 16, ticket 52: the drop-shadow
    /// ops the filter records reference).
    pub filter_op_chains: Vec<Vec<GpuiGoSceneFilterOpRecord>>,
    pub commands: Vec<GpuiGoSceneCommandRecord>,
    pub requirements: GpuiGoSceneRequirementsRecord,
}

pub(crate) fn snapshot_bounds(bounds: &GpuiGoSceneBounds) -> gpui::Bounds<gpui::ScaledPixels> {
    gpui::Bounds {
        origin: point(gpui::ScaledPixels(f32::from_bits(bounds.x)), gpui::ScaledPixels(f32::from_bits(bounds.y))),
        size: gpui::Size {
            width: gpui::ScaledPixels(f32::from_bits(bounds.w)),
            height: gpui::ScaledPixels(f32::from_bits(bounds.h)),
        },
    }
}

fn snapshot_mask(mask: &GpuiGoSceneContentMask) -> gpui::ContentMask<gpui::ScaledPixels> {
    // The pinned ContentMask carries the bounds and the per-edge fades
    // (the pin's shader evaluates them); the extended mask's rounded
    // clip applies in the VENDORED quad/shadow pipelines, which read
    // the ABI record directly (the pinned underline/sprite pipelines
    // clip rect-only this revision - the recorded gap handed to ticket
    // 52's mask work).
    gpui::ContentMask {
        bounds: snapshot_bounds(&mask.bounds),
        fade_out: gpui::Edges {
            top: gpui::ScaledPixels(f32::from_bits(mask.fade_out[1])),
            right: gpui::ScaledPixels(f32::from_bits(mask.fade_out[2])),
            bottom: gpui::ScaledPixels(f32::from_bits(mask.fade_out[3])),
            left: gpui::ScaledPixels(f32::from_bits(mask.fade_out[0])),
        },
    }
}

/// Multiplies a background record's alpha payload by `factor` (the
/// single-primitive opacity fold; both the solid alpha and the two
/// gradient stops' alphas).
fn fold_background(background: &mut GpuiGoSceneBackground, factor: f32) {
    background.a = (f32::from_bits(background.a) * factor).to_bits();
    background.stop0_a = (f32::from_bits(background.stop0_a) * factor).to_bits();
    background.stop1_a = (f32::from_bits(background.stop1_a) * factor).to_bits();
}

/// The snapshot's fold index: one factor per (primitive class, index),
/// built from the plan's fold list (rev 16, ticket 50).
#[derive(Default)]
struct FoldIndex {
    quads: Vec<Option<f32>>,
    shadows: Vec<Option<f32>>,
    underlines: Vec<Option<f32>>,
    paths: Vec<Option<f32>>,
    monochrome_sprites: Vec<Option<f32>>,
    subpixel_sprites: Vec<Option<f32>>,
    polychrome_sprites: Vec<Option<f32>>,
    surfaces: Vec<Option<f32>>,
    backdrops: Vec<Option<f32>>,
}

impl FoldIndex {
    fn build(folds: &[SceneFold], snapshot: &SceneRenderSnapshot) -> Self {
        let mut index = FoldIndex {
            quads: vec![None; snapshot.quads.len()],
            shadows: vec![None; snapshot.shadows.len()],
            underlines: vec![None; snapshot.underlines.len()],
            paths: vec![None; snapshot.paths.len()],
            monochrome_sprites: vec![None; snapshot.monochrome_sprites.len()],
            subpixel_sprites: vec![None; snapshot.subpixel_sprites.len()],
            polychrome_sprites: vec![None; snapshot.polychrome_sprites.len()],
            surfaces: vec![None; snapshot.surfaces.len()],
            backdrops: vec![None; snapshot.backdrops.len()],
        };
        for fold in folds {
            let slot = match fold.primitive {
                SceneFoldPrimitive::Quad => index.quads.get_mut(fold.index as usize),
                SceneFoldPrimitive::Shadow => index.shadows.get_mut(fold.index as usize),
                SceneFoldPrimitive::Underline => index.underlines.get_mut(fold.index as usize),
                SceneFoldPrimitive::Path => index.paths.get_mut(fold.index as usize),
                SceneFoldPrimitive::MonochromeSprite => {
                    index.monochrome_sprites.get_mut(fold.index as usize)
                }
                SceneFoldPrimitive::SubpixelSprite => {
                    index.subpixel_sprites.get_mut(fold.index as usize)
                }
                SceneFoldPrimitive::PolychromeSprite => {
                    index.polychrome_sprites.get_mut(fold.index as usize)
                }
                SceneFoldPrimitive::Surface => index.surfaces.get_mut(fold.index as usize),
                SceneFoldPrimitive::Backdrop => index.backdrops.get_mut(fold.index as usize),
            };
            if let Some(slot) = slot {
                // Nested folds on one primitive multiply (an opacity
                // group folded inside another folded group).
                *slot = Some(slot.unwrap_or(1.0) * fold.factor);
            }
        }
        index
    }
}

pub(crate) fn snapshot_radii(bits: &[u32]) -> gpui::Corners<gpui::ScaledPixels> {
    gpui::Corners {
        top_left: gpui::ScaledPixels(f32::from_bits(bits[0])),
        top_right: gpui::ScaledPixels(f32::from_bits(bits[1])),
        bottom_right: gpui::ScaledPixels(f32::from_bits(bits[2])),
        bottom_left: gpui::ScaledPixels(f32::from_bits(bits[3])),
    }
}


fn snapshot_color(color: &GpuiGoSceneBackground) -> gpui::Background {
    // The ABI carries the stored solid HSLA record or the two-stop
    // gradient record; both build through the pinned public
    // constructors (`Background::from(Hsla)` for the solid arm, the
    // pin's paint path, and `linear_gradient` + `color_space` for the
    // gradient arm, rev 15). The gradient's CSS geometry and
    // premultiplied sRGB mixing are evaluated by the vendored shader
    // (the recorded divergences in `shaders/mod.rs`); the kernel maps
    // the record field for field.
    match color.tag {
        0 => {
            let hsla: gpui::Hsla = gpui::Hsla::new(
                f32::from_bits(color.h) * 360.0,
                f32::from_bits(color.s),
                f32::from_bits(color.l),
                f32::from_bits(color.a),
            );
            gpui::Background::from(hsla)
        }
        _ => {
            let stop =
                |h: u32, s: u32, l: u32, a: u32, percentage: u32| -> gpui::LinearColorStop {
                    let hsla: gpui::Hsla = gpui::Hsla::new(
                        f32::from_bits(h) * 360.0,
                        f32::from_bits(s),
                        f32::from_bits(l),
                        f32::from_bits(a),
                    );
                    gpui::linear_color_stop(hsla, f32::from_bits(percentage))
                };
            gpui::linear_gradient(
                f32::from_bits(color.angle),
                stop(
                    color.stop0_h,
                    color.stop0_s,
                    color.stop0_l,
                    color.stop0_a,
                    color.stop0_percentage,
                ),
                stop(
                    color.stop1_h,
                    color.stop1_s,
                    color.stop1_l,
                    color.stop1_a,
                    color.stop1_percentage,
                ),
            )
            .color_space(match color.color_space {
                1 => gpui::ColorSpace::Oklab,
                _ => gpui::ColorSpace::Srgb,
            })
        }
    }
}

fn snapshot_scene_hsla(color: &GpuiGoSceneColor) -> gpui::Hsla {
    let hsla: gpui::Hsla = gpui::Hsla::new(
        f32::from_bits(color.h) * 360.0,
        f32::from_bits(color.s),
        f32::from_bits(color.l),
        f32::from_bits(color.a),
    );
    hsla
}

fn snapshot_tile(tile: &GpuiGoSceneTileRecord) -> gpui::AtlasTile {
    gpui::AtlasTile {
        texture_id: gpui::AtlasTextureId {
            index: tile.texture_index,
            kind: match tile.texture_kind {
                0 => gpui::AtlasTextureKind::Monochrome,
                1 => gpui::AtlasTextureKind::Polychrome,
                _ => gpui::AtlasTextureKind::Subpixel,
            },
        },
        tile_id: gpui::TileId(tile.tile_id),
        padding: tile.padding,
        bounds: gpui::Bounds {
            origin: point(
                gpui::DevicePixels(tile.bounds_x as i32),
                gpui::DevicePixels(tile.bounds_y as i32),
            ),
            size: gpui::Size {
                width: gpui::DevicePixels(tile.bounds_w as i32),
                height: gpui::DevicePixels(tile.bounds_h as i32),
            },
        },
    }
}

fn snapshot_transformation(bits: &[u32]) -> gpui::TransformationMatrix {
    gpui::TransformationMatrix {
        rotation_scale: [
            [f32::from_bits(bits[0]), f32::from_bits(bits[1])],
            [f32::from_bits(bits[2]), f32::from_bits(bits[3])],
        ],
        translation: [f32::from_bits(bits[4]), f32::from_bits(bits[5])],
    }
}

fn snapshot_underline(rec: &GpuiGoSceneUnderlineRecord) -> gpui::Underline {
    gpui::Underline {
        order: rec.order,
        padding: rec.padding,
        bounds: snapshot_bounds(&rec.bounds),
        content_mask: snapshot_mask(&rec.content_mask),
        color: gpui::Hsla::new(
            f32::from_bits(rec.color.h) * 360.0,
            f32::from_bits(rec.color.s),
            f32::from_bits(rec.color.l),
            f32::from_bits(rec.color.a),
        )
        .into(),
        thickness: gpui::ScaledPixels(f32::from_bits(rec.thickness)),
        wavy: gpui::ShaderBool::from(rec.wavy != 0),
    }
}

fn snapshot_mask_sprite(rec: &GpuiGoSceneSpriteRecord) -> gpui::MonochromeSprite {
    gpui::MonochromeSprite {
        order: rec.order,
        padding: rec.padding,
        bounds: snapshot_bounds(&rec.bounds),
        content_mask: snapshot_mask(&rec.content_mask),
        color: snapshot_scene_hsla(&rec.color).into(),
        tile: snapshot_tile(&rec.tile),
        transformation: snapshot_transformation(&rec.transformation),
    }
}

fn snapshot_subpixel_sprite(rec: &GpuiGoSceneSpriteRecord) -> gpui::SubpixelSprite {
    gpui::SubpixelSprite {
        order: rec.order,
        padding: rec.padding,
        bounds: snapshot_bounds(&rec.bounds),
        content_mask: snapshot_mask(&rec.content_mask),
        color: snapshot_scene_hsla(&rec.color).into(),
        tile: snapshot_tile(&rec.tile),
        transformation: snapshot_transformation(&rec.transformation),
    }
}

fn snapshot_polychrome_sprite(rec: &GpuiGoScenePolychromeSpriteRecord) -> gpui::PolychromeSprite {
    gpui::PolychromeSprite {
        order: rec.order,
        grayscale: gpui::ShaderBool::from(rec.grayscale != 0),
        opacity: f32::from_bits(rec.opacity),
        corner_smoothing: f32::from_bits(rec.corner_smoothing),
        bounds: snapshot_bounds(&rec.bounds),
        content_mask: snapshot_mask(&rec.content_mask),
        corner_radii: snapshot_radii(&rec.corner_radii),
        tile: snapshot_tile(&rec.tile),
    }
}

fn snapshot_path(data: &PathData) -> SnapshotPath {
    // The pinned `Path::clipped_bounds()` (bounds ∩ content mask) and
    // vertex set, precomputed for the renderer's rasterization and
    // sprite passes (the pinned `PathRasterizationVertex` construction
    // consumes exactly these fields).
    SnapshotPath {
        order: data.rec.order,
        clipped_bounds: snapshot_bounds(&clipped_path_bounds(data)),
        vertices: data
            .vertices
            .iter()
            .map(|vertex| SnapshotPathVertex {
                xy: point(
                    gpui::ScaledPixels(f32::from_bits(vertex.xy_x)),
                    gpui::ScaledPixels(f32::from_bits(vertex.xy_y)),
                ),
                st: gpui::Point::new(
                    f32::from_bits(vertex.st_u),
                    f32::from_bits(vertex.st_v),
                ),
            })
            .collect(),
        color: snapshot_color(&data.rec.color),
    }
}

/// The pinned `Path::clipped_bounds()`: `bounds.intersect(&content_mask.bounds)`.
fn clipped_path_bounds(data: &PathData) -> GpuiGoSceneBounds {
    let clipped = SceneState::clipped_bounds(&data.rec.bounds, &data.rec.content_mask.bounds);
    GpuiGoSceneBounds {
        x: clipped.origin_x.to_bits(),
        y: clipped.origin_y.to_bits(),
        w: clipped.width.to_bits(),
        h: clipped.height.to_bits(),
    }
}

/// One snapshot path: the draw-order, the precomputed clipped bounds,
/// the tessellated vertices and the fill color — the exact fields the
/// pinned renderer's path passes consume (`PathRasterizationVertex` and
/// `PathSprite`).
pub(crate) struct SnapshotPath {
    pub order: u32,
    pub clipped_bounds: gpui::Bounds<gpui::ScaledPixels>,
    pub vertices: Vec<SnapshotPathVertex>,
    pub color: gpui::Background,
}

/// One tessellated path vertex: xy (device pixels) and st (texture
/// coordinates).
pub(crate) struct SnapshotPathVertex {
    pub xy: gpui::Point<gpui::ScaledPixels>,
    pub st: gpui::Point<f32>,
}

fn snapshot_surface(rec: &GpuiGoSceneSurfaceRecord) -> GpuiGoSceneSurfaceRecord {
    // The ABI surface record IS the draw-side representation (the
    // pinned PaintSurface's observable fields — order, bounds, content
    // mask, source — map 1:1 onto it; the renderer service's surface
    // draw path consumes the record and resolves tag-1 capture frame
    // handles, preserving the pin's explicit import bail for tag-0
    // Unsupported sources as `renderer_status::ERR_UNSUPPORTED_SOURCE`).
    *rec
}

/// Resolves a scene handle and copies its finished draw data in the
/// pinned primitive layouts (the renderer service's draw entry calls
/// this; the snapshot owns every byte, so the scene lock is released
/// before the renderer lock is taken — no lock nesting).
pub(crate) fn scene_render_snapshot(handle: u64) -> Result<SceneRenderSnapshot, i32> {
    let shared = resolve_scene(handle)?;
    let state = lock_state(&shared);
    if state.failed {
        return Err(scene_status::ERR_SCENE_FAILED);
    }
    if !state.is_finished {
        return Err(scene_status::ERR_NOT_FINISHED);
    }
    Ok(render_snapshot_of_state(&state))
}

/// The snapshot builder over a locked, finished state (the tests drive
/// it directly on their SceneState).
fn render_snapshot_of_state(state: &SceneState) -> SceneRenderSnapshot {
    let mut snapshot = SceneRenderSnapshot {
        quads: state.quads.clone(),
        shadows: state.shadows.clone(),
        underlines: state.underlines.iter().map(snapshot_underline).collect(),
        monochrome_sprites: state
            .monochrome_sprites
            .iter()
            .map(snapshot_mask_sprite)
            .collect(),
        subpixel_sprites: state
            .subpixel_sprites
            .iter()
            .map(snapshot_subpixel_sprite)
            .collect(),
        polychrome_sprites: state
            .polychrome_sprites
            .iter()
            .map(snapshot_polychrome_sprite)
            .collect(),
        paths: state.paths.iter().map(snapshot_path).collect(),
        surfaces: state.surfaces.iter().map(snapshot_surface).collect(),
        surface_opacities: state.surface_opacities.clone(),
        backdrops: state.backdrops.clone(),
        boundaries: state.boundaries.clone(),
        effects: state.effects.clone(),
        gradients: state.gradients.iter().map(|g| g.rec.clone()).collect(),
        gradient_ramps: state
            .gradients
            .iter()
            .flat_map(|g| g.ramp.iter().copied())
            .collect(),
        coverage: state.coverage.clone(),
        filter_op_chains: state.filter_op_chains.clone(),
        commands: state.plan.commands.clone(),
        requirements: state.plan.requirements,
    };
    // The single-primitive opacity folds (rev 16, ticket 50): applied
    // to the SNAPSHOT's copies - the stored records stay exactly as
    // inserted, so dumps replay them unchanged and a re-finish rebuilds
    // the same plan.
    let folds = FoldIndex::build(&state.plan.folds, &snapshot);
    for (quad, factor) in snapshot.quads.iter_mut().zip(folds.quads.iter()) {
        if let Some(factor) = factor {
            fold_background(&mut quad.background, *factor);
            fold_background(&mut quad.border_color, *factor);
        }
    }
    for (shadow, factor) in snapshot.shadows.iter_mut().zip(folds.shadows.iter()) {
        if let Some(factor) = factor {
            fold_background(&mut shadow.color, *factor);
        }
    }
    for (underline, factor) in snapshot.underlines.iter_mut().zip(folds.underlines.iter()) {
        if let Some(factor) = factor {
            let mut hsla: gpui::Hsla = underline.color.into();
            hsla.alpha *= factor;
            underline.color = hsla.into();
        }
    }
    for (path, factor) in snapshot.paths.iter_mut().zip(folds.paths.iter()) {
        if let Some(factor) = factor {
            path.color = path.color.opacity(*factor);
        }
    }
    for (sprite, factor) in snapshot
        .monochrome_sprites
        .iter_mut()
        .zip(folds.monochrome_sprites.iter())
    {
        if let Some(factor) = factor {
            let mut hsla: gpui::Hsla = sprite.color.into();
            hsla.alpha *= factor;
            sprite.color = hsla.into();
        }
    }
    for (sprite, factor) in snapshot
        .subpixel_sprites
        .iter_mut()
        .zip(folds.subpixel_sprites.iter())
    {
        if let Some(factor) = factor {
            let mut hsla: gpui::Hsla = sprite.color.into();
            hsla.alpha *= factor;
            sprite.color = hsla.into();
        }
    }
    for (sprite, factor) in snapshot
        .polychrome_sprites
        .iter_mut()
        .zip(folds.polychrome_sprites.iter())
    {
        if let Some(factor) = factor {
            sprite.opacity *= factor;
        }
    }
    for (opacity, factor) in snapshot
        .surface_opacities
        .iter_mut()
        .zip(folds.surfaces.iter())
    {
        if let Some(factor) = factor {
            *opacity *= factor;
        }
    }
    for (backdrop, factor) in snapshot.backdrops.iter_mut().zip(folds.backdrops.iter()) {
        if let Some(factor) = factor {
            backdrop.opacity = (f32::from_bits(backdrop.opacity) * factor).to_bits();
        }
    }
    snapshot
}



/// The scene service table installed in reserved slot 3 of
/// `GpuiGoAbiTable`. All function pointers come first (alignment 8),
/// then the self-check scalars.
///
/// Version 6 (rev 16, ticket 50) appended `insert_effect` and
/// `effect_dump` after `insert_batch` (pointers @216 and @224; 29
/// pointers total) and three trailing self-check scalars:
/// `size_of_effect_record`, `size_of_content_mask`,
/// `max_scene_groups`.
///
/// Version 7 (rev 16, ticket 51) appended `update_property` after
/// `effect_dump` (pointer @232; 30 pointers, 240 bytes, the
/// self-check scalars moving to @240..320); total size 320, alignment
/// 8. The only entry addition of the version - no records, no
/// scalars changed.
#[repr(C)]
pub struct GpuiGoSceneTable {
    /// `scene_create`.
    pub create: Option<unsafe extern "system" fn(*mut u64) -> i32>,
    /// `scene_clear`.
    pub clear: Option<unsafe extern "system" fn(u64) -> i32>,
    /// `scene_dispose`.
    pub dispose: Option<unsafe extern "system" fn(u64) -> i32>,
    /// `scene_push_layer`.
    pub push_layer: Option<unsafe extern "system" fn(u64, *const GpuiGoSceneBounds) -> i32>,
    /// `scene_pop_layer`.
    pub pop_layer: Option<unsafe extern "system" fn(u64) -> i32>,
    /// `scene_raise_order_floor`.
    pub raise_order_floor: Option<unsafe extern "system" fn(u64) -> i32>,
    /// `scene_insert_quad`.
    pub insert_quad: Option<unsafe extern "system" fn(u64, *const GpuiGoSceneQuadRecord) -> i32>,
    /// `scene_insert_shadow`.
    pub insert_shadow: Option<unsafe extern "system" fn(u64, *const GpuiGoSceneShadowRecord) -> i32>,
    /// `scene_insert_underline`.
    pub insert_underline:
        Option<unsafe extern "system" fn(u64, *const GpuiGoSceneUnderlineRecord) -> i32>,
    /// `scene_insert_backdrop_filter`.
    pub insert_backdrop_filter:
        Option<unsafe extern "system" fn(u64, *const GpuiGoSceneFilterRecord) -> i32>,
    /// `scene_insert_filter_boundary`.
    pub insert_filter_boundary:
        Option<unsafe extern "system" fn(u64, *const GpuiGoSceneFilterRecord) -> i32>,
    /// `scene_insert_surface` (paired opacity).
    pub insert_surface:
        Option<unsafe extern "system" fn(u64, *const GpuiGoSceneSurfaceRecord, u32) -> i32>,
    /// `scene_insert_monochrome_sprite` (ticket10).
    pub insert_monochrome_sprite:
        Option<unsafe extern "system" fn(u64, *const GpuiGoSceneSpriteRecord) -> i32>,
    /// `scene_insert_subpixel_sprite` (ticket10).
    pub insert_subpixel_sprite:
        Option<unsafe extern "system" fn(u64, *const GpuiGoSceneSpriteRecord) -> i32>,
    /// `scene_insert_polychrome_sprite` (ticket10).
    pub insert_polychrome_sprite:
        Option<unsafe extern "system" fn(u64, *const GpuiGoScenePolychromeSpriteRecord) -> i32>,
    /// `scene_replay`.
    pub replay: Option<unsafe extern "system" fn(u64, u32, u32, u64) -> i32>,
    /// `scene_finish`.
    pub finish: Option<unsafe extern "system" fn(u64) -> i32>,
    /// `scene_len`.
    pub len: Option<unsafe extern "system" fn(u64, *mut u32) -> i32>,
    /// `scene_meta`.
    pub meta: Option<unsafe extern "system" fn(u64, *mut GpuiGoSceneMetaRecord) -> i32>,
    /// `scene_dump`.
    pub dump: Option<
        unsafe extern "system" fn(u64, u32, *mut u8, *mut u32, u32, *mut u32) -> i32,
    >,
    /// `scene_plan_dump`.
    pub plan_dump:
        Option<unsafe extern "system" fn(u64, *mut GpuiGoSceneCommandRecord, u32, *mut u32) -> i32>,
    /// `scene_requirements`.
    pub requirements:
        Option<unsafe extern "system" fn(u64, *mut GpuiGoSceneRequirementsRecord) -> i32>,
    /// `scene_panic_probe` (test-only; panics inside catch_unwind).
    pub panic_probe: Option<unsafe extern "system" fn() -> i32>,
    /// `scene_insert_path` (ticket16): the path primitive with its bulk
    /// vertex array.
    pub insert_path:
        Option<unsafe extern "system" fn(u64, *const GpuiGoScenePathRecord, *const GpuiGoScenePathVertexRecord, u32) -> i32>,
    /// `scene_path_dump` (ticket16): bulk path records + vertices.
    pub path_dump: Option<
        unsafe extern "system" fn(u64, *mut GpuiGoScenePathRecord, *mut GpuiGoScenePathVertexRecord, u32, u32, *mut u32, *mut u32) -> i32,
    >,
    /// `scene_path_script` (ticket16): the pinned PathBuilder
    /// tessellation (commands + word pool in, vertices + bounds out).
    pub path_script: Option<
        unsafe extern "system" fn(*const GpuiGoPathCommandRecord, u32, *const u32, u32, *mut GpuiGoScenePathVertexRecord, u32, *mut u32, *mut GpuiGoSceneBounds) -> i32,
    >,
    /// `scene_insert_batch` (service version 4, gpui-perf ticket 05):
    /// a packed op stream (see [`batch_op`]) applied in order in one call.
    pub insert_batch: Option<unsafe extern "system" fn(u64, *const u32, u32, *mut u32) -> i32>,
    /// `scene_insert_effect` (service version 6, rev 16 ticket 50): the
    /// effect-group marker insert (start or end, `flags.IS_START`).
    pub insert_effect:
        Option<unsafe extern "system" fn(u64, *const GpuiGoSceneEffectRecord) -> i32>,
    /// `scene_effect_dump` (service version 6, rev 16 ticket 50): the
    /// finished scene's effect records.
    pub effect_dump: Option<
        unsafe extern "system" fn(u64, *mut GpuiGoSceneEffectRecord, u32, *mut u32) -> i32,
    >,
    /// `scene_update_property` (service version 7, rev 16 ticket 51;
    /// the design's appendix A.7): mutates a finished scene's effect
    /// records carrying the node id in place (the matrix or the
    /// opacity) and reports the old and new mapped device bounds. No
    /// re-sort, no re-plan.
    pub update_property: Option<
        unsafe extern "system" fn(
            u64,
            u32,
            u32,
            *const u32,
            u32,
            *mut GpuiGoSceneBounds,
            *mut GpuiGoSceneBounds,
        ) -> i32,
    >,
    /// `scene_insert_gradient` (service version 8, rev 16 ticket 52):
    /// one gradient table entry plus its bulk stop array; the service
    /// bakes the 1024x1 premultiplied-sRGB ramp row and assigns
    /// `stop_row`.
    pub insert_gradient: Option<
        unsafe extern "system" fn(
            u64,
            *const GpuiGoSceneGradientRecord,
            *const GpuiGoSceneGradientStop,
            u32,
            *mut u32,
        ) -> i32,
    >,
    /// `scene_gradient_dump` (service version 8, rev 16 ticket 52): the
    /// finished scene's gradient records and concatenated stops.
    pub gradient_dump: Option<
        unsafe extern "system" fn(
            u64,
            *mut GpuiGoSceneGradientRecord,
            *mut GpuiGoSceneGradientStop,
            u32,
            u32,
            *mut u32,
            *mut u32,
        ) -> i32,
    >,
    /// `scene_insert_coverage` (service version 8, rev 16 ticket 52):
    /// one clip-path coverage entry (a path reference + bounds).
    pub insert_coverage:
        Option<unsafe extern "system" fn(u64, *const GpuiGoSceneCoverageRecord, *mut u32) -> i32>,
    /// `scene_insert_filter_op_chain` (service version 8, rev 16 ticket
    /// 52): the drop-shadow op chain the R15 filter records reference.
    pub insert_filter_op_chain: Option<
        unsafe extern "system" fn(
            u64,
            *const GpuiGoSceneFilterOpRecord,
            u32,
            *mut u32,
        ) -> i32,
    >,
    /// Scene ABI version (see [`GPUI_GO_SCENE_SERVICE_VERSION`]).
    pub service_version: u32,
    /// `size_of::<GpuiGoSceneTable>()` self-check.
    pub size_of_table: u32,
    /// `align_of::<GpuiGoSceneTable>()` self-check.
    pub align_of_table: u32,
    /// `size_of::<GpuiGoSceneQuadRecord>()` self-check.
    pub size_of_quad_record: u32,
    /// `size_of::<GpuiGoSceneShadowRecord>()` self-check.
    pub size_of_shadow_record: u32,
    /// `size_of::<GpuiGoSceneUnderlineRecord>()` self-check.
    pub size_of_underline_record: u32,
    /// `size_of::<GpuiGoSceneFilterRecord>()` self-check.
    pub size_of_filter_record: u32,
    /// `size_of::<GpuiGoSceneSurfaceRecord>()` self-check.
    pub size_of_surface_record: u32,
    /// `size_of::<GpuiGoSceneSpriteRecord>()` self-check (monochrome and
    /// subpixel share the layout).
    pub size_of_monochrome_sprite_record: u32,
    /// `size_of::<GpuiGoScenePolychromeSpriteRecord>()` self-check.
    pub size_of_polychrome_sprite_record: u32,
    /// `size_of::<GpuiGoSceneCommandRecord>()` self-check.
    pub size_of_command_record: u32,
    /// `size_of::<GpuiGoSceneRequirementsRecord>()` self-check.
    pub size_of_requirements_record: u32,
    /// `size_of::<GpuiGoSceneMetaRecord>()` self-check.
    pub size_of_meta_record: u32,
    /// `size_of::<GpuiGoScenePathRecord>()` self-check (ticket16).
    pub size_of_path_record: u32,
    /// `size_of::<GpuiGoScenePathVertexRecord>()` self-check (ticket16).
    pub size_of_path_vertex_record: u32,
    /// `size_of::<GpuiGoPathCommandRecord>()` self-check (ticket16).
    pub size_of_path_command_record: u32,
    /// Maximum filters in one chain (the pinned SmallVec capacity).
    pub max_filters: u32,
    /// `size_of::<GpuiGoSceneEffectRecord>()` self-check (rev 16).
    pub size_of_effect_record: u32,
    /// `size_of::<GpuiGoSceneContentMask>()` self-check (rev 16).
    pub size_of_content_mask: u32,
    /// The group-count sanity bound `MAX_SCENE_GROUPS` (rev 16).
    pub max_scene_groups: u32,
    /// `size_of::<GpuiGoSceneGradientRecord>()` self-check (rev 16,
    /// ticket 52).
    pub size_of_gradient_record: u32,
    /// `size_of::<GpuiGoSceneGradientStop>()` self-check (rev 16,
    /// ticket 52).
    pub size_of_gradient_stop: u32,
    /// `MAX_GRADIENT_STOPS` (rev 16, ticket 52: 64, the user's
    /// decision).
    pub max_gradient_stops: u32,
    /// `size_of::<GpuiGoSceneCoverageRecord>()` self-check (rev 16,
    /// ticket 52).
    pub size_of_coverage_record: u32,
    /// `size_of::<GpuiGoSceneFilterOpRecord>()` self-check (rev 16,
    /// ticket 52).
    pub size_of_filter_op_record: u32,
}

pub(crate) static SCENE_TABLE: GpuiGoSceneTable = GpuiGoSceneTable {
    create: Some(scene_create),
    clear: Some(scene_clear),
    dispose: Some(scene_dispose),
    push_layer: Some(scene_push_layer),
    pop_layer: Some(scene_pop_layer),
    raise_order_floor: Some(scene_raise_order_floor),
    insert_quad: Some(scene_insert_quad),
    insert_shadow: Some(scene_insert_shadow),
    insert_underline: Some(scene_insert_underline),
    insert_backdrop_filter: Some(scene_insert_backdrop_filter),
    insert_filter_boundary: Some(scene_insert_filter_boundary),
    insert_surface: Some(scene_insert_surface),
    insert_monochrome_sprite: Some(scene_insert_monochrome_sprite),
    insert_subpixel_sprite: Some(scene_insert_subpixel_sprite),
    insert_polychrome_sprite: Some(scene_insert_polychrome_sprite),
    replay: Some(scene_replay),
    finish: Some(scene_finish),
    len: Some(scene_len),
    meta: Some(scene_meta),
    dump: Some(scene_dump),
    plan_dump: Some(scene_plan_dump),
    requirements: Some(scene_requirements),
    panic_probe: Some(scene_panic_probe),
    insert_path: Some(scene_insert_path),
    path_dump: Some(scene_path_dump),
    path_script: Some(scene_path_script),
    insert_batch: Some(scene_insert_batch),
    insert_effect: Some(scene_insert_effect),
    effect_dump: Some(scene_effect_dump),
    update_property: Some(scene_update_property),
    insert_gradient: Some(scene_insert_gradient),
    gradient_dump: Some(scene_gradient_dump),
    insert_coverage: Some(scene_insert_coverage),
    insert_filter_op_chain: Some(scene_insert_filter_op_chain),
    service_version: GPUI_GO_SCENE_SERVICE_VERSION,
    size_of_table: core::mem::size_of::<GpuiGoSceneTable>() as u32,
    align_of_table: core::mem::align_of::<GpuiGoSceneTable>() as u32,
    size_of_quad_record: core::mem::size_of::<GpuiGoSceneQuadRecord>() as u32,
    size_of_shadow_record: core::mem::size_of::<GpuiGoSceneShadowRecord>() as u32,
    size_of_underline_record: core::mem::size_of::<GpuiGoSceneUnderlineRecord>() as u32,
    size_of_filter_record: core::mem::size_of::<GpuiGoSceneFilterRecord>() as u32,
    size_of_surface_record: core::mem::size_of::<GpuiGoSceneSurfaceRecord>() as u32,
    size_of_monochrome_sprite_record: core::mem::size_of::<GpuiGoSceneSpriteRecord>() as u32,
    size_of_polychrome_sprite_record:
        core::mem::size_of::<GpuiGoScenePolychromeSpriteRecord>() as u32,
    size_of_command_record: core::mem::size_of::<GpuiGoSceneCommandRecord>() as u32,
    size_of_requirements_record: core::mem::size_of::<GpuiGoSceneRequirementsRecord>() as u32,
    size_of_meta_record: core::mem::size_of::<GpuiGoSceneMetaRecord>() as u32,
    size_of_path_record: core::mem::size_of::<GpuiGoScenePathRecord>() as u32,
    size_of_path_vertex_record: core::mem::size_of::<GpuiGoScenePathVertexRecord>() as u32,
    size_of_path_command_record: core::mem::size_of::<GpuiGoPathCommandRecord>() as u32,
    max_filters: MAX_SCENE_FILTERS,
    size_of_effect_record: core::mem::size_of::<GpuiGoSceneEffectRecord>() as u32,
    size_of_content_mask: core::mem::size_of::<GpuiGoSceneContentMask>() as u32,
    max_scene_groups: MAX_SCENE_GROUPS,
    size_of_gradient_record: core::mem::size_of::<GpuiGoSceneGradientRecord>() as u32,
    size_of_gradient_stop: core::mem::size_of::<GpuiGoSceneGradientStop>() as u32,
    max_gradient_stops: MAX_GRADIENT_STOPS,
    size_of_coverage_record: core::mem::size_of::<GpuiGoSceneCoverageRecord>() as u32,
    size_of_filter_op_record: core::mem::size_of::<GpuiGoSceneFilterOpRecord>() as u32,
};

// ---------------------------------------------------------------------------
// Table entries (panic boundary wrappers)
// ---------------------------------------------------------------------------

unsafe extern "system" fn scene_create(out_handle: *mut u64) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| scene_create_body(out_handle)));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            scene_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn scene_clear(handle: u64) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| scene_clear_body(handle)));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_scene_failed(handle);
            scene_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn scene_dispose(handle: u64) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| scene_dispose_body(handle)));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_scene_failed(handle);
            scene_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn scene_push_layer(handle: u64, bounds: *const GpuiGoSceneBounds) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| scene_push_layer_body(handle, bounds)));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_scene_failed(handle);
            scene_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn scene_pop_layer(handle: u64) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| scene_pop_layer_body(handle)));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_scene_failed(handle);
            scene_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn scene_raise_order_floor(handle: u64) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| scene_raise_order_floor_body(handle)));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_scene_failed(handle);
            scene_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn scene_insert_batch(
    handle: u64,
    words: *const u32,
    word_count: u32,
    out_applied: *mut u32,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| {
        scene_insert_batch_body(handle, words, word_count, out_applied)
    }));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_scene_failed(handle);
            scene_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn scene_insert_quad(handle: u64, rec: *const GpuiGoSceneQuadRecord) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| scene_insert_quad_body(handle, rec)));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_scene_failed(handle);
            scene_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn scene_insert_shadow(
    handle: u64,
    rec: *const GpuiGoSceneShadowRecord,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| scene_insert_shadow_body(handle, rec)));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_scene_failed(handle);
            scene_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn scene_insert_underline(
    handle: u64,
    rec: *const GpuiGoSceneUnderlineRecord,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| scene_insert_underline_body(handle, rec)));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_scene_failed(handle);
            scene_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn scene_insert_backdrop_filter(
    handle: u64,
    rec: *const GpuiGoSceneFilterRecord,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| {
        scene_insert_backdrop_body(handle, rec)
    }));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_scene_failed(handle);
            scene_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn scene_insert_filter_boundary(
    handle: u64,
    rec: *const GpuiGoSceneFilterRecord,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| {
        scene_insert_boundary_body(handle, rec)
    }));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_scene_failed(handle);
            scene_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn scene_insert_surface(
    handle: u64,
    rec: *const GpuiGoSceneSurfaceRecord,
    opacity_bits: u32,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| {
        scene_insert_surface_body(handle, rec, opacity_bits)
    }));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_scene_failed(handle);
            scene_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn scene_insert_monochrome_sprite(
    handle: u64,
    rec: *const GpuiGoSceneSpriteRecord,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| {
        scene_insert_monochrome_sprite_body(handle, rec)
    }));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_scene_failed(handle);
            scene_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn scene_insert_subpixel_sprite(
    handle: u64,
    rec: *const GpuiGoSceneSpriteRecord,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| {
        scene_insert_subpixel_sprite_body(handle, rec)
    }));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_scene_failed(handle);
            scene_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn scene_insert_polychrome_sprite(
    handle: u64,
    rec: *const GpuiGoScenePolychromeSpriteRecord,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| {
        scene_insert_polychrome_sprite_body(handle, rec)
    }));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_scene_failed(handle);
            scene_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn scene_insert_effect(handle: u64, rec: *const GpuiGoSceneEffectRecord) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| scene_insert_effect_body(handle, rec)));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_scene_failed(handle);
            scene_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn scene_effect_dump(
    handle: u64,
    records: *mut GpuiGoSceneEffectRecord,
    capacity: u32,
    out_count: *mut u32,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| {
        scene_effect_dump_body(handle, records, capacity, out_count)
    }));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            scene_status::ERR_PANIC
        }
    }
}

/// `scene_update_property` (rev 16, ticket 51): the panic-boundary
/// wrapper over [`scene_update_property_body`].
unsafe extern "system" fn scene_update_property(
    handle: u64,
    node_id: u32,
    kind: u32,
    values: *const u32,
    value_count: u32,
    out_old: *mut GpuiGoSceneBounds,
    out_new: *mut GpuiGoSceneBounds,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| {
        scene_update_property_body(
            handle,
            node_id,
            kind,
            values,
            value_count,
            out_old,
            out_new,
        )
    }));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            scene_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn scene_insert_gradient(
    handle: u64,
    rec: *const GpuiGoSceneGradientRecord,
    stops: *const GpuiGoSceneGradientStop,
    stop_count: u32,
    out_index: *mut u32,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| {
        scene_insert_gradient_body(handle, rec, stops, stop_count, out_index)
    }));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_scene_failed(handle);
            scene_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn scene_gradient_dump(
    handle: u64,
    records: *mut GpuiGoSceneGradientRecord,
    stops: *mut GpuiGoSceneGradientStop,
    record_capacity: u32,
    stop_capacity: u32,
    out_record_count: *mut u32,
    out_stop_count: *mut u32,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| {
        scene_gradient_dump_body(
            handle,
            records,
            stops,
            record_capacity,
            stop_capacity,
            out_record_count,
            out_stop_count,
        )
    }));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_scene_failed(handle);
            scene_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn scene_insert_coverage(
    handle: u64,
    rec: *const GpuiGoSceneCoverageRecord,
    out_index: *mut u32,
) -> i32 {
    let outcome =
        catch_unwind(AssertUnwindSafe(|| scene_insert_coverage_body(handle, rec, out_index)));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_scene_failed(handle);
            scene_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn scene_insert_filter_op_chain(
    handle: u64,
    ops: *const GpuiGoSceneFilterOpRecord,
    op_count: u32,
    out_index: *mut u32,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| {
        scene_insert_filter_op_chain_body(handle, ops, op_count, out_index)
    }));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_scene_failed(handle);
            scene_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn scene_replay(handle: u64, start: u32, end: u32, prev: u64) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| {
        scene_replay_body(handle, start, end, prev)
    }));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_scene_failed(handle);
            scene_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn scene_finish(handle: u64) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| scene_finish_body(handle)));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_scene_failed(handle);
            scene_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn scene_len(handle: u64, out_len: *mut u32) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| scene_len_body(handle, out_len)));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_scene_failed(handle);
            scene_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn scene_meta(handle: u64, out_meta: *mut GpuiGoSceneMetaRecord) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| scene_meta_body(handle, out_meta)));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_scene_failed(handle);
            scene_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn scene_dump(
    handle: u64,
    kind: u32,
    records: *mut u8,
    opacity_bits: *mut u32,
    capacity: u32,
    out_count: *mut u32,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| {
        scene_dump_body(handle, kind, records, opacity_bits, capacity, out_count)
    }));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_scene_failed(handle);
            scene_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn scene_plan_dump(
    handle: u64,
    commands: *mut GpuiGoSceneCommandRecord,
    capacity: u32,
    out_count: *mut u32,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| {
        scene_plan_dump_body(handle, commands, capacity, out_count)
    }));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_scene_failed(handle);
            scene_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn scene_requirements(
    handle: u64,
    out: *mut GpuiGoSceneRequirementsRecord,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| scene_requirements_body(handle, out)));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_scene_failed(handle);
            scene_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn scene_panic_probe() -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| scene_panic_probe_body()));
    match outcome {
        // Unreachable: the body always panics.
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            scene_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn scene_insert_path(
    handle: u64,
    rec: *const GpuiGoScenePathRecord,
    vertices: *const GpuiGoScenePathVertexRecord,
    vertex_count: u32,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| {
        scene_insert_path_body(handle, rec, vertices, vertex_count)
    }));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_scene_failed(handle);
            scene_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn scene_path_dump(
    handle: u64,
    records: *mut GpuiGoScenePathRecord,
    vertices: *mut GpuiGoScenePathVertexRecord,
    record_capacity: u32,
    vertex_capacity: u32,
    out_record_count: *mut u32,
    out_vertex_count: *mut u32,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| {
        scene_path_dump_body(
            handle,
            records,
            vertices,
            record_capacity,
            vertex_capacity,
            out_record_count,
            out_vertex_count,
        )
    }));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_scene_failed(handle);
            scene_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn scene_path_script(
    commands: *const GpuiGoPathCommandRecord,
    command_count: u32,
    points: *const u32,
    word_count: u32,
    out_vertices: *mut GpuiGoScenePathVertexRecord,
    vertex_capacity: u32,
    out_vertex_count: *mut u32,
    out_bounds: *mut GpuiGoSceneBounds,
) -> i32 {
    // The tessellation entry is stateless (no scene handle, no scene
    // failure to mark); a contained panic still maps to ERR_PANIC.
    let outcome = catch_unwind(AssertUnwindSafe(|| {
        scene_path_script_body(
            commands,
            command_count,
            points,
            word_count,
            out_vertices,
            vertex_capacity,
            out_vertex_count,
            out_bounds,
        )
    }));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            scene_status::ERR_PANIC
        }
    }
}

// ---------------------------------------------------------------------------
// Tests (hand-computed expectations derived from the pinned source)
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;

    fn sp(value: f32) -> GpuiGoSceneBounds {
        GpuiGoSceneBounds::from_f32(F32Bounds {
            origin_x: 0.0,
            origin_y: 0.0,
            width: value,
            height: value,
        })
    }

    /// Full 100x100 bounds at the origin, the pinned test shape.
    fn full_bounds() -> GpuiGoSceneBounds {
        GpuiGoSceneBounds::from_f32(F32Bounds {
            origin_x: 0.0,
            origin_y: 0.0,
            width: 100.0,
            height: 100.0,
        })
    }

    fn mask() -> GpuiGoSceneContentMask {
        GpuiGoSceneContentMask {
            bounds: full_bounds(),
            ..Default::default()
        }
    }

    fn quad(smoothing: f32) -> GpuiGoSceneQuadRecord {
        GpuiGoSceneQuadRecord {
            bounds: full_bounds(),
            content_mask: mask(),
            corner_smoothing: smoothing.to_bits(),
            ..Default::default()
        }
    }

    /// A 100x100 quad whose bounds don't overlap `full_bounds()` (the
    /// pinned detached_quad: exercises order reuse).
    fn detached_quad() -> GpuiGoSceneQuadRecord {
        let bounds = GpuiGoSceneBounds::from_f32(F32Bounds {
            origin_x: 200.0,
            origin_y: 200.0,
            width: 100.0,
            height: 100.0,
        });
        GpuiGoSceneQuadRecord {
            content_mask: GpuiGoSceneContentMask {
                bounds,
                ..Default::default()
            },
            bounds,
            ..Default::default()
        }
    }

    fn shadow(smoothing: f32) -> GpuiGoSceneShadowRecord {
        GpuiGoSceneShadowRecord {
            blur_radius: 0.0f32.to_bits(),
            bounds: full_bounds(),
            corner_radii: [0; 4],
            content_mask: mask(),
            element_bounds: full_bounds(),
            element_corner_radii: [0; 4],
            inset: 0,
            corner_smoothing: smoothing.to_bits(),
            ..Default::default()
        }
    }

    fn underline() -> GpuiGoSceneUnderlineRecord {
        GpuiGoSceneUnderlineRecord {
            bounds: full_bounds(),
            content_mask: mask(),
            thickness: 1.0f32.to_bits(),
            ..Default::default()
        }
    }

    fn boundary(is_start: bool) -> GpuiGoSceneFilterRecord {
        GpuiGoSceneFilterRecord {
            bounds: full_bounds(),
            content_mask: mask(),
            blur_radii: [8.0f32.to_bits(), 0, 0, 0, 0, 0, 0, 0],
            filter_count: 1,
            opacity: 1.0f32.to_bits(),
            is_start: u32::from(is_start),
            ..Default::default()
        }
    }

    fn backdrop() -> GpuiGoSceneFilterRecord {
        GpuiGoSceneFilterRecord {
            bounds: full_bounds(),
            content_mask: mask(),
            blur_radii: [20.0f32.to_bits(), 0, 0, 0, 0, 0, 0, 0],
            filter_count: 1,
            opacity: 1.0f32.to_bits(),
            ..Default::default()
        }
    }

    fn surface() -> GpuiGoSceneSurfaceRecord {
        GpuiGoSceneSurfaceRecord {
            bounds: full_bounds(),
            content_mask: mask(),
            ..Default::default()
        }
    }

    fn new_scene() -> SceneState {
        SceneState::default()
    }

    /// Command signatures in plan order, e.g. "quad 0..2",
    /// "begin:Isolated(0)", "end:Inline[1->4]".
    fn command_signature(scene: &mut SceneState) -> Vec<String> {
        scene.finish().unwrap();
        scene
            .plan
            .commands
            .iter()
            .map(|command| {
                if command.command_kind == command_kind::BATCH {
                    let kind = match command.primitive_kind {
                        batch_primitive_kind::SHADOWS => {
                            if command.smoothed != 0 { "smoothed shadow" } else { "shadow" }
                        }
                        batch_primitive_kind::QUADS => {
                            if command.smoothed != 0 { "smoothed quad" } else { "quad" }
                        }
                        batch_primitive_kind::UNDERLINES => "underline",
                        batch_primitive_kind::SURFACES => "surface",
                        batch_primitive_kind::BACKDROP_FILTERS => "backdrop",
                        _ => "other",
                    };
                    format!("{} {}..{}", kind, command.range_start, command.range_end)
                } else if command.command_kind == command_kind::BEGIN_FILTER {
                    format!("begin:{}", target_name(command.filter_target, command.target_index))
                } else if command.command_kind == command_kind::BEGIN_EFFECT {
                    format!(
                        "begin-effect:{}",
                        target_name(command.filter_target, command.target_index)
                    )
                } else if command.command_kind == command_kind::END_EFFECT {
                    format!(
                        "end-effect:{}[{}->{}]",
                        target_name(command.filter_target, command.target_index),
                        command.boundary_index,
                        command.closing_boundary_index
                    )
                } else {
                    format!(
                        "end:{}[{}->{}]",
                        target_name(command.filter_target, command.target_index),
                        command.boundary_index,
                        command.closing_boundary_index
                    )
                }
            })
            .collect()
    }

    fn target_name(target: u32, index: u32) -> String {
        if target == 0 {
            "Inline".to_string()
        } else {
            format!("Isolated({index})")
        }
    }

    #[test]
    fn bounds_tree_matches_the_pinned_insert_orders() {
        // The pinned bounds_tree test_insert: overlapping inserts step,
        // non-overlapping inserts reuse low orders.
        let mut tree = BoundsTree::default();
        let b = |x: f32, y: f32| F32Bounds { origin_x: x, origin_y: y, width: 10.0, height: 10.0 };
        assert_eq!(tree.insert(b(0.0, 0.0)).unwrap(), 1);
        assert_eq!(tree.insert(b(5.0, 5.0)).unwrap(), 2);
        assert_eq!(tree.insert(b(10.0, 10.0)).unwrap(), 3);
        assert_eq!(tree.insert(b(20.0, 20.0)).unwrap(), 1);
        assert_eq!(tree.insert(b(40.0, 40.0)).unwrap(), 1);
        assert_eq!(tree.insert(b(25.0, 25.0)).unwrap(), 2);
    }

    #[test]
    fn bounds_tree_random_inserts_match_max_intersecting_plus_one() {
        // The pinned randomized check, condensed: for random AABBs the
        // assigned order is one greater than the max order among
        // intersecting bounds (or the floor).
        use std::cell::Cell;
        // A tiny xorshift RNG keeps this deterministic without a rand
        // dependency.
        struct Rng(Cell<u64>);
        impl Rng {
            fn next_f32(&self, lo: f32, hi: f32) -> f32 {
                let mut x = self.0.get();
                x ^= x << 13;
                x ^= x >> 7;
                x ^= x << 17;
                self.0.set(x);
                let unit = (x >> 40) as f32 / (1u64 << 24) as f32;
                lo + unit * (hi - lo)
            }
        }
        for seed in 1..=50u64 {
            let rng = Rng(Cell::new(seed | 1));
            let mut tree = BoundsTree::default();
            let mut inserted: Vec<(F32Bounds, u32)> = Vec::new();
            for _ in 0..200 {
                let bounds = F32Bounds {
                    origin_x: rng.next_f32(-100.0, 100.0),
                    origin_y: rng.next_f32(-100.0, 100.0),
                    width: rng.next_f32(0.0, 50.0),
                    height: rng.next_f32(0.0, 50.0),
                };
                let expected = inserted
                    .iter()
                    .filter(|(b, _)| b.intersects(&bounds))
                    .map(|(_, order)| *order)
                    .max()
                    .unwrap_or(0)
                    + 1;
                let actual = tree.insert(bounds).unwrap();
                assert_eq!(actual, expected, "seed {seed}");
                inserted.push((bounds, actual));
            }
        }
    }

    #[test]
    fn smoothing_splits_quads_and_shadows_into_batches() {
        // Derived from the pinned smoothing_and_texture_changes_define_batches
        // (the polychrome-sprite half is sprite work and omitted).
        let mut scene = new_scene();
        for smoothing in [0.0, 0.0, 0.5, 1.0, 0.0] {
            scene.insert_quad(quad(smoothing)).unwrap();
        }
        for smoothing in [0.0, 0.0, 0.5, 1.0, 0.0] {
            scene.insert_shadow(shadow(smoothing)).unwrap();
        }
        let signatures = command_signature(&mut scene);
        assert_eq!(
            signatures,
            vec![
                "quad 0..2",
                "smoothed quad 2..4",
                "quad 4..5",
                "shadow 0..2",
                "smoothed shadow 2..4",
                "shadow 4..5",
            ]
        );
        // Every primitive covers the same bounds so the orders are
        // strictly increasing in insertion order (the pinned full_bounds
        // property).
        assert_eq!(scene.quads.iter().map(|q| q.order).collect::<Vec<_>>(), vec![1, 2, 3, 4, 5]);
        assert_eq!(
            scene.shadows.iter().map(|s| s.order).collect::<Vec<_>>(),
            vec![6, 7, 8, 9, 10]
        );
    }

    #[test]
    fn content_filter_group_brackets_its_children() {
        // The pinned test: start, child, end. A matched group takes the
        // first isolated target.
        let mut scene = new_scene();
        scene.insert_quad(quad(0.0)).unwrap();
        scene.insert_boundary(boundary(true)).unwrap();
        scene.insert_quad(quad(0.0)).unwrap();
        scene.insert_boundary(boundary(false)).unwrap();
        assert_eq!(
            command_signature(&mut scene),
            vec![
                "quad 0..1",
                "begin:Isolated(0)",
                "quad 1..2",
                "end:Isolated(0)[0->1]",
            ]
        );
    }

    #[test]
    fn nested_content_filters_emit_well_nested_ordering() {
        // The pinned test: start, child, start, child, end, end — with
        // isolated targets 0 and 1 for the matched nested groups.
        let mut scene = new_scene();
        scene.insert_boundary(boundary(true)).unwrap();
        scene.insert_quad(quad(0.0)).unwrap();
        scene.insert_boundary(boundary(true)).unwrap();
        scene.insert_quad(quad(0.0)).unwrap();
        scene.insert_boundary(boundary(false)).unwrap();
        scene.insert_boundary(boundary(false)).unwrap();
        assert_eq!(
            command_signature(&mut scene),
            vec![
                "begin:Isolated(0)",
                "quad 0..1",
                "begin:Isolated(1)",
                "quad 1..2",
                "end:Isolated(1)[1->2]",
                "end:Isolated(0)[0->3]",
            ]
        );
    }

    #[test]
    fn content_after_a_filter_group_sorts_above_it() {
        // The pinned test: a non-overlapping sibling painted after the
        // group must sort after the end marker (the close-time floor).
        let mut scene = new_scene();
        scene.insert_boundary(boundary(true)).unwrap();
        scene.insert_quad(quad(0.0)).unwrap();
        scene.insert_boundary(boundary(false)).unwrap();
        scene.insert_quad(detached_quad()).unwrap();
        assert_eq!(
            command_signature(&mut scene),
            vec![
                "begin:Isolated(0)",
                "quad 0..1",
                "end:Isolated(0)[0->1]",
                "quad 1..2",
            ]
        );
    }

    #[test]
    fn backdrop_filter_sorts_before_a_later_overlapping_quad() {
        let mut scene = new_scene();
        scene.insert_quad(quad(0.0)).unwrap();
        scene.insert_backdrop(backdrop()).unwrap();
        scene.insert_quad(quad(0.0)).unwrap();
        assert_eq!(
            command_signature(&mut scene),
            vec!["quad 0..1", "backdrop 0..1", "quad 1..2"]
        );
    }

    #[test]
    fn render_commands_pair_nested_filters_and_bound_isolation_targets() {
        // The pinned three-nested-group test, updated for rev 16 (ticket
        // 50): targets 0, 1, 2 - NO depth cap (the pre-R16 third group
        // rendered inline, unfiltered; that divergence is the ticket's
        // point). The first two keep EXACTLY the R15 plan (indices 0
        // and 1); each end carries its matched start's index.
        let mut scene = new_scene();
        scene.insert_boundary(boundary(true)).unwrap();
        scene.insert_quad(quad(0.0)).unwrap();
        scene.insert_boundary(boundary(true)).unwrap();
        scene.insert_quad(quad(0.0)).unwrap();
        scene.insert_boundary(boundary(true)).unwrap();
        scene.insert_quad(quad(0.0)).unwrap();
        scene.insert_boundary(boundary(false)).unwrap();
        scene.insert_boundary(boundary(false)).unwrap();
        scene.insert_boundary(boundary(false)).unwrap();
        assert_eq!(
            command_signature(&mut scene),
            vec![
                "begin:Isolated(0)",
                "quad 0..1",
                "begin:Isolated(1)",
                "quad 1..2",
                "begin:Isolated(2)",
                "quad 2..3",
                "end:Isolated(2)[2->3]",
                "end:Isolated(1)[1->4]",
                "end:Isolated(0)[0->5]",
            ]
        );
    }

    fn effect(is_start: bool, flags: u32) -> GpuiGoSceneEffectRecord {
        GpuiGoSceneEffectRecord {
            bounds: full_bounds(),
            content_mask: full_bounds(),
            flags: flags | u32::from(is_start) * effect_flags::IS_START,
            opacity: 0.5f32.to_bits(),
            ..Default::default()
        }
    }

    #[test]
    fn effect_groups_pair_through_the_shared_group_stack() {
        // A matched effect pair composites through an isolated target
        // slot, nested with a filter pair: the slots count OPEN groups
        // of either kind (the filter takes 0, the effect 1) - the same
        // stack, no depth cap.
        let mut scene = new_scene();
        scene.insert_boundary(boundary(true)).unwrap();
        scene.insert_effect(effect(true, effect_flags::HAS_OPACITY)).unwrap();
        scene.insert_quad(quad(0.0)).unwrap();
        scene.insert_quad(quad(0.0)).unwrap();
        scene.insert_effect(effect(false, effect_flags::HAS_OPACITY)).unwrap();
        scene.insert_boundary(boundary(false)).unwrap();
        // The two quads share one batch (the pinned merge rule: a batch
        // extends while the next primitive stays under the following
        // marker's order).
        assert_eq!(
            command_signature(&mut scene),
            vec![
                "begin:Isolated(0)",
                "begin-effect:Isolated(1)",
                "quad 0..2",
                "end-effect:Isolated(1)[0->1]",
                "end:Isolated(0)[0->1]",
            ]
        );
        let requirements = scene.plan.requirements;
        assert_eq!(requirements.isolated_target_count, 2);
        assert_eq!(requirements.isolated_effect_count, 1);
        assert_eq!(requirements.isolated_filter_count, 1);
        assert_eq!(scene.effects.len(), 2);
        // The effect markers survive with kernel-assigned orders.
        assert!(scene.effects[0].order < scene.effects[1].order);
    }

    #[test]
    fn single_primitive_opacity_group_folds_inline() {
        // The fold: a pure-opacity effect group whose span holds exactly
        // ONE primitive emits INLINE begin/end commands and the opacity
        // multiplies that primitive's colour at snapshot time (the
        // stored record stays as inserted - the dump reads it
        // unchanged).
        let mut scene = new_scene();
        scene.insert_effect(effect(true, effect_flags::HAS_OPACITY)).unwrap();
        let mut opaque = quad(0.0);
        opaque.background.a = 1.0f32.to_bits();
        opaque.border_color.a = 1.0f32.to_bits();
        scene.insert_quad(opaque).unwrap();
        scene.insert_effect(effect(false, effect_flags::HAS_OPACITY)).unwrap();
        assert_eq!(
            command_signature(&mut scene),
            vec![
                "begin-effect:Inline",
                "quad 0..1",
                "end-effect:Inline[0->1]",
            ]
        );
        // The fold is recorded for the snapshot.
        assert_eq!(scene.plan.folds.len(), 1);
        assert_eq!(scene.plan.folds[0].primitive, SceneFoldPrimitive::Quad);
        assert_eq!(scene.plan.folds[0].index, 0);
        assert_eq!(scene.plan.folds[0].factor, 0.5);
        // The STORED record is unmultiplied (dumps read it unchanged).
        assert_eq!(f32::from_bits(scene.quads[0].background.a), 1.0);
        // The snapshot applies the fold.
        let snapshot = render_snapshot_of_state(&scene);
        assert_eq!(f32::from_bits(snapshot.quads[0].background.a), 0.5);
    }

    #[test]
    fn single_primitive_group_with_clip_does_not_fold() {
        // A clipping group (rounded clip) does NOT fold even with one
        // primitive: the clip needs the isolated composite.
        let mut scene = new_scene();
        scene
            .insert_effect(effect(true, effect_flags::HAS_OPACITY | effect_flags::CLIP_TO_BOUNDS))
            .unwrap();
        scene.insert_quad(quad(0.0)).unwrap();
        scene
            .insert_effect(effect(false, effect_flags::HAS_OPACITY | effect_flags::CLIP_TO_BOUNDS))
            .unwrap();
        assert_eq!(
            command_signature(&mut scene),
            vec![
                "begin-effect:Isolated(0)",
                "quad 0..1",
                "end-effect:Isolated(0)[0->1]",
            ]
        );
        assert!(scene.plan.folds.is_empty());
    }

    #[test]
    fn unmatched_effect_end_emits_an_inline_end() {
        // The boundary machinery applies verbatim: an unmatched end
        // renders inline.
        let mut scene = new_scene();
        scene.insert_quad(quad(0.0)).unwrap();
        scene.insert_effect(effect(false, effect_flags::HAS_OPACITY)).unwrap();
        assert_eq!(
            command_signature(&mut scene),
            vec!["quad 0..1", "end-effect:Inline[0->0]"]
        );
    }

    #[test]
    fn deep_effect_nesting_gets_targets_beyond_the_old_cap() {
        // Depth 6: all six groups isolate (the pre-R16 cap was 2 - for
        // FILTER groups; effect groups never existed).
        let mut scene = new_scene();
        for _ in 0..6 {
            scene.insert_effect(effect(true, effect_flags::HAS_OPACITY)).unwrap();
            scene.insert_quad(quad(0.0)).unwrap();
        }
        for _ in 0..6 {
            scene.insert_effect(effect(false, effect_flags::HAS_OPACITY)).unwrap();
        }
        scene.finish().unwrap();
        let commands = &scene.plan.commands;
        let begins: Vec<_> = commands
            .iter()
            .filter(|command| command.command_kind == command_kind::BEGIN_EFFECT)
            .collect();
        assert_eq!(begins.len(), 6);
        // The five OUTER groups isolate with nesting slots 0..4; the
        // innermost (whose span is exactly its one quad) FOLDS to
        // inline, its begin patched back.
        for (depth, command) in begins.iter().take(5).enumerate() {
            assert_eq!(command.filter_target, 1);
            assert_eq!(command.target_index, depth as u32);
        }
        assert_eq!(begins[5].filter_target, 0);
        // The requirement's high-water estimate counts every isolated
        // begin (a conservative upper bound - the fold never opens it).
        assert_eq!(scene.plan.requirements.isolated_target_count, 6);
        // Exactly one fold: the innermost quad (the outer spans hold
        // nested markers).
        assert_eq!(scene.plan.folds.len(), 1);
        assert_eq!(scene.plan.folds[0].primitive, SceneFoldPrimitive::Quad);
        assert_eq!(scene.plan.folds[0].index, 5);
    }

    #[test]
    fn scene_groups_limit_returns_err_limit() {
        // MAX_SCENE_GROUPS is the ABI sanity bound: the start marker
        // that would exceed it fails with ERR_LIMIT, and the boundary
        // starts count toward the same bound (both kinds, one stack).
        let mut scene = new_scene();
        for _ in 0..MAX_SCENE_GROUPS {
            scene.insert_effect(effect(true, effect_flags::HAS_OPACITY)).unwrap();
        }
        let overflow = effect(true, effect_flags::HAS_OPACITY);
        let result = scene.insert_effect(overflow);
        assert_eq!(result, Err(scene_status::ERR_LIMIT));
        // End markers never count.
        for _ in 0..4 {
            scene
                .insert_effect(effect(false, effect_flags::HAS_OPACITY))
                .unwrap();
        }
        // Boundary starts share the bound: with MAX_SCENE_GROUPS effect
        // starts already inserted, a boundary start is over it too.
        let result = scene.insert_boundary(boundary(true));
        assert_eq!(result, Err(scene_status::ERR_LIMIT));
    }

    #[test]
    fn effect_record_validation_rejects_malformed_inserts() {
        // Validation lives at the ABI boundary (the bodies), exactly
        // like every other record: unknown flag bits, a singular
        // transform, a mask KIND out of range (the kinds 1-3 are wired
        // since ticket 52; a kind-0 mask with a payload still
        // rejects), a property node without an id (or without a
        // value), opacity out of range and negative radii all fail
        // with ERR_BAD_VALUE.
        let mut handle = 0u64;
        assert_eq!(scene_create_body(&mut handle), scene_status::OK);
        let rejects = |record: GpuiGoSceneEffectRecord| {
            assert_eq!(
                scene_insert_effect_body(handle, &record),
                scene_status::ERR_BAD_VALUE
            );
        };
        let mut record = effect(true, effect_flags::HAS_OPACITY);
        record.flags |= 1 << 9;
        rejects(record);

        let mut record = effect(true, effect_flags::HAS_TRANSFORM);
        record.transform = [0; 6];
        rejects(record);

        let mut record = effect(true, effect_flags::HAS_OPACITY);
        // rev 16, ticket 52: the kinds 1-3 validate (the renderer
        // resolves the reference); kind 4 and beyond reject.
        record.mask_kind = 4;
        rejects(record);

        let mut record = effect(true, effect_flags::HAS_OPACITY);
        // A none-kind mask with a reference still rejects.
        record.mask_kind = 0;
        record.mask_reference = 1;
        rejects(record);

        let mut record = effect(true, effect_flags::PROPERTY_NODE | effect_flags::HAS_OPACITY);
        record.node_id = 0;
        rejects(record);

        let mut record = effect(true, effect_flags::PROPERTY_NODE);
        record.node_id = 7;
        rejects(record);

        let mut record = effect(true, effect_flags::HAS_OPACITY);
        record.opacity = 1.5f32.to_bits();
        rejects(record);

        let mut record = effect(true, effect_flags::HAS_OPACITY);
        record.corner_radii = [(-1.0f32).to_bits(), 0, 0, 0];
        rejects(record);

        let mut record = effect(true, effect_flags::HAS_OPACITY);
        record.reserved = 1;
        rejects(record);
        assert_eq!(scene_dispose_body(handle), scene_status::OK);
    }

    #[test]
    fn extended_content_mask_validates_and_round_trips() {
        // Fades, radii and smoothing ride every record that has a mask;
        // the validation rejects negative fades, negative radii and
        // out-of-range smoothing.
        let mut scene = new_scene();
        let mut record = quad(0.0);
        record.content_mask.fade_out = [4.0f32.to_bits(), 0, 0, 8.0f32.to_bits()];
        record.content_mask.corner_radii = [6.0f32.to_bits(); 4];
        record.content_mask.corner_smoothing = 0.25f32.to_bits();
        scene.insert_quad(record).unwrap();
        scene.finish().unwrap();
        assert_eq!(scene.quads[0].content_mask, record.content_mask);

        // Validation lives at the ABI boundary (the bodies).
        let mut handle = 0u64;
        assert_eq!(scene_create_body(&mut handle), scene_status::OK);
        let mut bad = quad(0.0);
        bad.content_mask.fade_out = [(-1.0f32).to_bits(), 0, 0, 0];
        assert_eq!(scene_insert_quad_body(handle, &bad), scene_status::ERR_BAD_VALUE);
        let mut bad = quad(0.0);
        bad.content_mask.corner_radii = [(-2.0f32).to_bits(), 0, 0, 0];
        assert_eq!(scene_insert_quad_body(handle, &bad), scene_status::ERR_BAD_VALUE);
        let mut bad = quad(0.0);
        bad.content_mask.corner_smoothing = 1.5f32.to_bits();
        assert_eq!(scene_insert_quad_body(handle, &bad), scene_status::ERR_BAD_VALUE);
        assert_eq!(scene_dispose_body(handle), scene_status::OK);
    }

    #[test]
    fn effect_records_replay_and_sort_like_boundaries() {
        // Replay re-inserts effect markers with recomputed orders; the
        // start-before-end tie-break applies to the effect array.
        let mut scene = new_scene();
        scene.insert_effect(effect(true, effect_flags::HAS_OPACITY)).unwrap();
        scene.insert_quad(quad(0.0)).unwrap();
        scene.insert_quad(quad(0.0)).unwrap();
        scene.insert_effect(effect(false, effect_flags::HAS_OPACITY)).unwrap();
        scene.finish().unwrap();
        let ops = scene.ops.clone();
        let mut replayed = new_scene();
        replayed.replay(0, ops.len(), ops).unwrap();
        replayed.finish().unwrap();
        assert_eq!(replayed.effects.len(), 2);
        assert_eq!(
            command_signature(&mut replayed),
            vec![
                "begin-effect:Isolated(0)",
                "quad 0..2",
                "end-effect:Isolated(0)[0->1]",
            ]
        );
        // The span's two primitives merge into one batch, so the group
        // does NOT fold (a two-primitive span needs the group).
        assert!(replayed.plan.folds.is_empty());
    }

    #[test]
    fn unmatched_effect_start_renders_inline() {
        let mut scene = new_scene();
        scene.insert_effect(effect(true, effect_flags::HAS_OPACITY)).unwrap();
        scene.insert_quad(quad(0.0)).unwrap();
        scene.insert_quad(quad(0.0)).unwrap();
        assert_eq!(
            command_signature(&mut scene),
            vec!["begin-effect:Inline", "quad 0..2"]
        );
    }

    #[test]
    fn unmatched_filter_start_renders_inline() {
        // The pinned test: an unmatched start renders inline, emits its
        // children, and no end command follows.
        let mut scene = new_scene();
        scene.insert_boundary(boundary(true)).unwrap();
        scene.insert_quad(quad(0.0)).unwrap();
        scene.finish().unwrap();
        assert_eq!(scene.plan.commands.len(), 2);
        assert_eq!(scene.plan.commands[0].command_kind, command_kind::BEGIN_FILTER);
        assert_eq!(scene.plan.commands[0].filter_target, 0);
        assert_eq!(scene.plan.commands[1].command_kind, command_kind::BATCH);
        assert_eq!(scene.plan.requirements.uses_offscreen_target, 0);
    }

    #[test]
    fn unmatched_filter_end_emits_an_inline_end() {
        // The pinned plan's unmatched-end fallback: an end without a start
        // emits an inline end command (debug_assert in debug builds).
        let mut scene = new_scene();
        scene.insert_quad(quad(0.0)).unwrap();
        scene.insert_boundary(boundary(false)).unwrap();
        let commands = command_signature(&mut scene);
        assert_eq!(commands, vec!["quad 0..1", "end:Inline[0->0]"]);
    }

    #[test]
    fn surface_opacity_is_preserved_without_changing_paint_surface_layout() {
        // The pinned test: paired opacities survive finish (the sort pairs
        // surfaces with their opacities) and replay.
        let mut scene = new_scene();
        scene.insert_surface(surface(), 0.25).unwrap();
        scene.insert_surface(surface(), 1.0).unwrap();
        scene.finish().unwrap();
        assert_eq!(scene.surface_opacities, vec![0.25, 1.0]);

        let ops = scene.ops.clone();
        let mut replay = new_scene();
        replay.replay(0, ops.len(), ops).unwrap();
        replay.finish().unwrap();
        assert_eq!(replay.surface_opacities, vec![0.25, 1.0]);
    }

    #[test]
    fn surface_opacity_pairs_survive_reordering() {
        // Two overlapping surfaces inserted with different opacities: the
        // pair must stay together through the finish sort.
        let mut scene = new_scene();
        scene.insert_surface(surface(), 0.9).unwrap();
        scene.insert_surface(surface(), 0.25).unwrap();
        scene.finish().unwrap();
        // Both cover the same bounds, so orders are 1 then 2 (sorted
        // ascending), and each surface keeps its own opacity.
        assert_eq!(
            scene.surfaces.iter().map(|s| s.order).collect::<Vec<_>>(),
            vec![1, 2]
        );
        assert_eq!(scene.surface_opacities, vec![0.9, 0.25]);
    }

    #[test]
    fn plan_requirements_are_collected_with_filter_pairing() {
        // The pinned test.
        let mut scene = new_scene();
        scene.insert_backdrop(backdrop()).unwrap();
        scene.insert_boundary(boundary(true)).unwrap();
        scene.insert_quad(quad(0.0)).unwrap();
        scene.insert_boundary(boundary(false)).unwrap();
        scene.finish().unwrap();
        let requirements = &scene.plan.requirements;
        assert_eq!(requirements.backdrop_filter_count, 1);
        assert_eq!(requirements.isolated_filter_count, 1);
        assert_eq!(requirements.isolated_target_count, 1);
        assert_eq!(requirements.instance_batch_count, 1);
        assert_eq!(requirements.uses_offscreen_target, 1);
    }

    #[test]
    fn layer_primitives_carry_the_layer_order() {
        // A layer is a batch of geometry sharing one draw order: the
        // layer's own order.
        let mut scene = new_scene();
        scene.insert_quad(quad(0.0)).unwrap(); // order 1
        scene
            .push_layer(F32Bounds { origin_x: 0.0, origin_y: 0.0, width: 100.0, height: 100.0 })
            .unwrap(); // order 2
        scene.insert_quad(quad(0.0)).unwrap(); // order 2 (layer order)
        scene.insert_quad(quad(0.0)).unwrap(); // order 2 (layer order)
        scene.pop_layer().unwrap();
        scene.insert_quad(quad(0.0)).unwrap(); // order 3
        scene.finish().unwrap();
        assert_eq!(scene.quads.iter().map(|q| q.order).collect::<Vec<_>>(), vec![1, 2, 2, 3]);
        // All four quads share one kind and no other kind is live, so the
        // plan merges them into one batch (batches only split at kind
        // boundaries, smoothing or texture changes).
        assert_eq!(scene.plan.commands.len(), 1);
        assert_eq!(scene.plan.commands[0].range_start, 0);
        assert_eq!(scene.plan.commands[0].range_end, 4);
    }

    #[test]
    fn raise_order_floor_puts_deferred_draws_above_everything() {
        // The deferred-draw floor: overlays (and their backdrops) sort
        // above the main scene.
        let mut scene = new_scene();
        scene.insert_quad(quad(0.0)).unwrap(); // order 1
        scene.insert_quad(detached_quad()).unwrap(); // order 1 (reused, no overlap)
        scene.raise_order_floor().unwrap(); // floor = 2
        scene.insert_quad(detached_quad()).unwrap(); // would reuse 1 without the floor
        scene.finish().unwrap();
        assert_eq!(scene.quads.iter().map(|q| q.order).collect::<Vec<_>>(), vec![1, 1, 2]);
        // With only quads live, the plan emits one merged batch; the
        // order array is the real observable (the overlay sorts last).
        assert_eq!(scene.plan.commands.len(), 1);
        assert_eq!(scene.plan.commands[0].range_end, 3);
    }

    #[test]
    fn empty_clipped_primitives_are_dropped_but_boundaries_survive() {
        // A primitive whose clipped bounds are empty disappears; a filter
        // boundary survives with its primitive bounds.
        let mut scene = new_scene();
        let mut out_of_mask = quad(0.0);
        out_of_mask.bounds = GpuiGoSceneBounds::from_f32(F32Bounds {
            origin_x: 200.0,
            origin_y: 200.0,
            width: 50.0,
            height: 50.0,
        });
        scene.insert_quad(out_of_mask).unwrap();
        assert_eq!(scene.quads.len(), 0);
        assert_eq!(scene.ops.len(), 0);

        // A boundary with the same fully-outside bounds still inserts,
        // with its primitive bounds (not the empty clipped bounds).
        let mut boundary_out = boundary(true);
        boundary_out.bounds = out_of_mask.bounds;
        boundary_out.content_mask = mask();
        scene.insert_boundary(boundary_out).unwrap();
        assert_eq!(scene.boundaries.len(), 1);
        let stored = &scene.boundaries[0];
        assert_eq!(f32::from_bits(stored.bounds.x), 200.0);
        scene.finish().unwrap();
        assert_eq!(scene.plan.commands.len(), 1);
        assert_eq!(scene.plan.commands[0].command_kind, command_kind::BEGIN_FILTER);
    }

    #[test]
    fn replay_recomputes_orders_against_the_target_tree() {
        // Replayed operations are re-inserted: their orders are computed
        // against the TARGET scene's bounds tree and layer state.
        let mut source = new_scene();
        source.insert_quad(quad(0.0)).unwrap(); // order 1
        source.insert_quad(quad(0.0)).unwrap(); // order 2
        source
            .push_layer(F32Bounds { origin_x: 0.0, origin_y: 0.0, width: 100.0, height: 100.0 })
            .unwrap(); // order 3
        source.insert_quad(quad(0.0)).unwrap(); // order 3 (layer)
        source.pop_layer().unwrap();
        source.finish().unwrap();
        let ops = source.ops.clone();

        let mut target = new_scene();
        target.insert_quad(detached_quad()).unwrap(); // order 1 (target's own tree)
        target.replay(0, ops.len(), ops).unwrap();
        target.finish().unwrap();
        // The detached quad does not overlap the full bounds, so the first
        // replayed quad reuses order 1; the second steps over it; the
        // replayed layer push then steps to 3 and its member carries 3.
        assert_eq!(
            target.quads.iter().map(|q| q.order).collect::<Vec<_>>(),
            vec![1, 1, 2, 3]
        );
    }

    #[test]
    fn kind_tiebreak_at_equal_order_follows_the_pinned_discriminants() {
        // Primitives at non-overlapping locations reuse order 1, so the
        // plan's ordering at that order is the PrimitiveKind discriminant
        // order: Shadow < Quad < Underline < Surface < BackdropFilter.
        // (rev 16 inserted EffectGroupStart just under FilterBoundaryStart
        // and EffectGroupEnd just over FilterBoundaryEnd; the RELATIVE
        // order of the pre-existing kinds is unchanged, so no pre-R16
        // scene can reorder.) The boundaries take orders above all prior
        // content (2 and 3) and bracket nothing in this degenerate shape.
        let wide_bounds = GpuiGoSceneBounds::from_f32(F32Bounds {
            origin_x: 0.0,
            origin_y: 0.0,
            width: 1000.0,
            height: 1000.0,
        });
        let wide_mask = GpuiGoSceneContentMask {
            bounds: wide_bounds,
            ..Default::default()
        };
        let at = |x: f32| {
            GpuiGoSceneBounds::from_f32(F32Bounds {
                origin_x: x,
                origin_y: 0.0,
                width: 10.0,
                height: 10.0,
            })
        };
        let mut scene = new_scene();
        scene.insert_quad(GpuiGoSceneQuadRecord {
            bounds: at(0.0),
            content_mask: wide_mask,
            ..Default::default()
        })
        .unwrap();
        scene.insert_underline(GpuiGoSceneUnderlineRecord {
            bounds: at(20.0),
            content_mask: wide_mask,
            thickness: 1.0f32.to_bits(),
            ..Default::default()
        })
        .unwrap();
        scene.insert_shadow(GpuiGoSceneShadowRecord {
            bounds: at(40.0),
            content_mask: wide_mask,
            element_bounds: at(40.0),
            ..Default::default()
        })
        .unwrap();
        scene.insert_surface(
            GpuiGoSceneSurfaceRecord {
                bounds: at(60.0),
                content_mask: wide_mask,
                ..Default::default()
            },
            0.5,
        )
        .unwrap();
        scene.insert_backdrop(GpuiGoSceneFilterRecord {
            bounds: at(80.0),
            content_mask: wide_mask,
            blur_radii: [4.0f32.to_bits(), 0, 0, 0, 0, 0, 0, 0],
            filter_count: 1,
            opacity: 1.0f32.to_bits(),
            ..Default::default()
        })
        .unwrap();
        // All five share order 1.
        assert_eq!(scene.quads[0].order, 1);
        assert_eq!(scene.underlines[0].order, 1);
        assert_eq!(scene.shadows[0].order, 1);
        assert_eq!(scene.surfaces[0].order, 1);
        assert_eq!(scene.backdrops[0].order, 1);
        // Boundaries above all prior content: orders 2 and 3.
        scene.insert_boundary(boundary(true)).unwrap();
        scene.insert_boundary(boundary(false)).unwrap();
        assert_eq!(scene.boundaries[0].order, 2);
        assert_eq!(scene.boundaries[1].order, 3);
        assert_eq!(
            command_signature(&mut scene),
            vec![
                "shadow 0..1",
                "quad 0..1",
                "underline 0..1",
                "surface 0..1",
                "backdrop 0..1",
                "begin:Isolated(0)",
                "end:Isolated(0)[0->1]",
            ]
        );
    }

    #[test]
    fn record_validation_rejects_malformed_inserts() {
        // Validation lives at the ABI boundary (the bodies); replayed
        // records legitimately carry a non-zero order field, so the state
        // methods accept any record and overwrite the order.
        let mut handle = 0u64;
        assert_eq!(scene_create_body(&mut handle), scene_status::OK);
        // Non-zero order on insert.
        let mut bad = quad(0.0);
        bad.order = 7;
        assert_eq!(scene_insert_quad_body(handle, &bad), scene_status::ERR_BAD_VALUE);
        // Record-size mismatch.
        let mut bad = quad(0.0);
        bad.record_size = 0;
        assert_eq!(scene_insert_quad_body(handle, &bad), scene_status::ERR_BAD_VALUE);
        // Unsupported color tag.
        let mut bad = quad(0.0);
        bad.background.tag = 1;
        assert_eq!(scene_insert_quad_body(handle, &bad), scene_status::ERR_BAD_VALUE);
        // Bad boolean.
        let mut bad = shadow(0.0);
        bad.inset = 2;
        assert_eq!(scene_insert_shadow_body(handle, &bad), scene_status::ERR_BAD_VALUE);
        // Filter chain over the bound.
        let mut bad = backdrop();
        bad.filter_count = MAX_SCENE_FILTERS + 1;
        assert_eq!(scene_insert_backdrop_body(handle, &bad), scene_status::ERR_BAD_VALUE);
        // Reserved surface payload must be zero.
        let mut bad = surface();
        bad.source_reserved = [1, 0, 0];
        assert_eq!(
            scene_insert_surface_body(handle, &bad, 1.0f32.to_bits()),
            scene_status::ERR_BAD_VALUE
        );
        // Nothing was published by the rejected inserts.
        let mut out_meta = GpuiGoSceneMetaRecord::default();
        assert_eq!(scene_meta_body(handle, &mut out_meta), scene_status::OK);
        assert_eq!(out_meta.op_count, 0);
        assert_eq!(out_meta.quad_count, 0);
        assert_eq!(scene_dispose_body(handle), scene_status::OK);
    }

    #[test]
    fn scene_ops_len_counts_paint_operations_only() {
        let mut scene = new_scene();
        scene.insert_quad(quad(0.0)).unwrap();
        scene
            .push_layer(F32Bounds { origin_x: 0.0, origin_y: 0.0, width: 10.0, height: 10.0 })
            .unwrap();
        scene.insert_quad(quad(0.0)).unwrap();
        scene.pop_layer().unwrap();
        scene.raise_order_floor().unwrap();
        // 2 primitives + layer open/close; the floor is not an operation.
        assert_eq!(scene.ops.len(), 4);
        // A dropped (empty clipped) primitive adds no operation.
        let mut out_of_mask = quad(0.0);
        out_of_mask.bounds = GpuiGoSceneBounds::from_f32(F32Bounds {
            origin_x: 500.0,
            origin_y: 500.0,
            width: 10.0,
            height: 10.0,
        });
        scene.insert_quad(out_of_mask).unwrap();
        assert_eq!(scene.ops.len(), 4);
    }

    #[test]
    fn service_dump_requires_finish_and_reports_capacity() {
        // The dump path through the ABI body: a scene handle from
        // scene_create_body, insert, dump before finish
        // (ERR_NOT_FINISHED), capacity retry, then a successful dump.
        let mut handle = 0u64;
        assert_eq!(scene_create_body(&mut handle), scene_status::OK);

        let rec = quad(0.0);
        assert_eq!(scene_insert_quad_body(handle, &rec), scene_status::OK);

        // Before finish: not finished.
        let mut out_count = 0u32;
        let mut out_meta = GpuiGoSceneMetaRecord::default();
        let mut buffer = [0u8; core::mem::size_of::<GpuiGoSceneQuadRecord>()];
        let mut opacity = [0u32; 1];
        assert_eq!(
            scene_dump_body(
                handle,
                dump_kind::QUADS,
                buffer.as_mut_ptr(),
                opacity.as_mut_ptr(),
                1,
                &mut out_count
            ),
            scene_status::ERR_NOT_FINISHED
        );

        assert_eq!(scene_finish_body(handle), scene_status::OK);

        // Zero capacity reports the required count.
        out_count = 0;
        assert_eq!(
            scene_dump_body(
                handle,
                dump_kind::QUADS,
                core::ptr::null_mut(),
                core::ptr::null_mut(),
                0,
                &mut out_count
            ),
            scene_status::ERR_CAPACITY
        );
        assert_eq!(out_count, 1);

        // Unknown kind.
        assert_eq!(
            scene_dump_body(handle, 99, buffer.as_mut_ptr(), opacity.as_mut_ptr(), 1, &mut out_count),
            scene_status::ERR_BAD_VALUE
        );

        // Successful dump copies the record with the assigned order.
        assert_eq!(
            scene_dump_body(
                handle,
                dump_kind::QUADS,
                buffer.as_mut_ptr(),
                opacity.as_mut_ptr(),
                1,
                &mut out_count
            ),
            scene_status::OK
        );
        assert_eq!(out_count, 1);
        let dumped = unsafe { *(buffer.as_ptr() as *const GpuiGoSceneQuadRecord) };
        assert_eq!(dumped.order, 1);
        assert_eq!(
            dumped.record_size,
            core::mem::size_of::<GpuiGoSceneQuadRecord>() as u32
        );

        // Meta and requirements agree.
        assert_eq!(scene_meta_body(handle, &mut out_meta), scene_status::OK);
        assert_eq!(out_meta.record_size, core::mem::size_of::<GpuiGoSceneMetaRecord>() as u32);
        assert_eq!(out_meta.op_count, 1);
        assert_eq!(out_meta.quad_count, 1);
        assert_eq!(out_meta.layer_push_count, 0);
        assert_eq!(out_meta.is_finished, 1);
        let mut requirements = GpuiGoSceneRequirementsRecord::default();
        assert_eq!(scene_requirements_body(handle, &mut requirements), scene_status::OK);
        assert_eq!(requirements.command_count, 1);
        assert_eq!(requirements.instance_batch_count, 1);

        // Plan dump with capacity retry.
        out_count = 0;
        assert_eq!(
            scene_plan_dump_body(handle, core::ptr::null_mut(), 0, &mut out_count),
            scene_status::ERR_CAPACITY
        );
        assert_eq!(out_count, 1);
        let mut commands = [GpuiGoSceneCommandRecord::default()];
        assert_eq!(
            scene_plan_dump_body(handle, commands.as_mut_ptr(), 1, &mut out_count),
            scene_status::OK
        );
        assert_eq!(commands[0].command_kind, command_kind::BATCH);
        assert_eq!(commands[0].primitive_kind, batch_primitive_kind::QUADS);
        assert_eq!(commands[0].range_start, 0);
        assert_eq!(commands[0].range_end, 1);

        assert_eq!(scene_dispose_body(handle), scene_status::OK);
        // Stale after dispose.
        assert_eq!(
            scene_insert_quad_body(handle, &quad(0.0)),
            scene_status::ERR_STALE_HANDLE
        );
    }

    #[test]
    fn service_replay_rejects_self_replay_and_bad_ranges() {
        let mut handle = 0u64;
        assert_eq!(scene_create_body(&mut handle), scene_status::OK);
        let mut other = 0u64;
        assert_eq!(scene_create_body(&mut other), scene_status::OK);
        let rec = quad(0.0);
        assert_eq!(scene_insert_quad_body(handle, &rec), scene_status::OK);

        // A scene cannot replay itself (the pinned signature borrows the
        // source immutably and the target mutably).
        assert_eq!(scene_replay_body(handle, 0, 1, handle), scene_status::ERR_BAD_VALUE);
        // Out-of-range end.
        assert_eq!(scene_replay_body(other, 0, 5, handle), scene_status::ERR_BAD_VALUE);
        // Inverted range.
        assert_eq!(scene_replay_body(other, 1, 0, handle), scene_status::ERR_BAD_VALUE);
        // A valid replay of one op.
        assert_eq!(scene_replay_body(other, 0, 1, handle), scene_status::OK);
        let mut out_meta = GpuiGoSceneMetaRecord::default();
        assert_eq!(scene_meta_body(other, &mut out_meta), scene_status::OK);
        assert_eq!(out_meta.op_count, 1);

        // Stale source.
        let mut stale = 0u64;
        assert_eq!(scene_create_body(&mut stale), scene_status::OK);
        assert_eq!(scene_dispose_body(stale), scene_status::OK);
        assert_eq!(
            scene_replay_body(other, 0, 1, stale),
            scene_status::ERR_STALE_HANDLE
        );

        assert_eq!(scene_dispose_body(handle), scene_status::OK);
        assert_eq!(scene_dispose_body(other), scene_status::OK);
    }

    #[test]
    fn service_clear_resets_the_scene_for_reuse() {
        let mut handle = 0u64;
        assert_eq!(scene_create_body(&mut handle), scene_status::OK);
        assert_eq!(scene_insert_quad_body(handle, &quad(0.0)), scene_status::OK);
        assert_eq!(scene_finish_body(handle), scene_status::OK);
        assert_eq!(scene_clear_body(handle), scene_status::OK);
        let mut out_meta = GpuiGoSceneMetaRecord::default();
        assert_eq!(scene_meta_body(handle, &mut out_meta), scene_status::OK);
        assert_eq!(out_meta.op_count, 0);
        assert_eq!(out_meta.is_finished, 0);
        // The scene is usable again after clear.
        assert_eq!(scene_insert_quad_body(handle, &quad(0.0)), scene_status::OK);
        assert_eq!(scene_finish_body(handle), scene_status::OK);
        assert_eq!(scene_dispose_body(handle), scene_status::OK);
    }

    #[test]
    fn service_panic_probe_reports_containment() {
        // Through the table wrapper (the panic boundary), like the Go
        // conformance tests do.
        let probe = SCENE_TABLE.panic_probe.unwrap();
        assert_eq!(unsafe { probe() }, scene_status::ERR_PANIC);
    }

    #[test]
    fn table_identity_constants() {
        assert_eq!(SCENE_TABLE.service_version, GPUI_GO_SCENE_SERVICE_VERSION);
        assert!(SCENE_TABLE.create.is_some());
        assert!(SCENE_TABLE.panic_probe.is_some());
        assert_eq!(
            SCENE_TABLE.size_of_quad_record as usize,
            core::mem::size_of::<GpuiGoSceneQuadRecord>()
        );
        assert_eq!(
            SCENE_TABLE.size_of_shadow_record as usize,
            core::mem::size_of::<GpuiGoSceneShadowRecord>()
        );
        assert_eq!(
            SCENE_TABLE.size_of_underline_record as usize,
            core::mem::size_of::<GpuiGoSceneUnderlineRecord>()
        );
        assert_eq!(
            SCENE_TABLE.size_of_filter_record as usize,
            core::mem::size_of::<GpuiGoSceneFilterRecord>()
        );
        assert_eq!(
            SCENE_TABLE.size_of_surface_record as usize,
            core::mem::size_of::<GpuiGoSceneSurfaceRecord>()
        );
        assert_eq!(
            SCENE_TABLE.size_of_command_record as usize,
            core::mem::size_of::<GpuiGoSceneCommandRecord>()
        );
        assert_eq!(
            SCENE_TABLE.size_of_requirements_record as usize,
            core::mem::size_of::<GpuiGoSceneRequirementsRecord>()
        );
        assert_eq!(
            SCENE_TABLE.size_of_meta_record as usize,
            core::mem::size_of::<GpuiGoSceneMetaRecord>()
        );
        assert_eq!(SCENE_TABLE.max_filters, MAX_SCENE_FILTERS);
    }

    #[test]
    fn sp_helper_roundtrips_bits() {
        let bounds = sp(12.5);
        assert_eq!(f32::from_bits(bounds.w), 12.5);
        assert_eq!(f32::from_bits(bounds.h), 12.5);
    }

    // -----------------------------------------------------------------------
    // Path primitives (ticket16)
    // -----------------------------------------------------------------------

    fn path_record(bounds: &GpuiGoSceneBounds, vertices: usize) -> GpuiGoScenePathRecord {
        GpuiGoScenePathRecord {
            bounds: *bounds,
            content_mask: mask(),
            color: GpuiGoSceneBackground {
                tag: 0,
                h: 0.5f32.to_bits(),
                s: 0.5f32.to_bits(),
                l: 0.5f32.to_bits(),
                a: 1.0f32.to_bits(),
                ..Default::default()
            },
            vertex_count: vertices as u32,
            ..Default::default()
        }
    }

    fn triangle_vertices(x: f32, y: f32) -> Vec<GpuiGoScenePathVertexRecord> {
        let point = |dx: f32, dy: f32| GpuiGoScenePathVertexRecord {
            xy_x: (x + dx).to_bits(),
            xy_y: (y + dy).to_bits(),
            st_u: 0.0f32.to_bits(),
            st_v: 1.0f32.to_bits(),
        };
        vec![point(0.0, 0.0), point(10.0, 0.0), point(0.0, 10.0)]
    }

    #[test]
    fn paths_sort_by_order_and_merge_into_batches() {
        // Overlapping paths step their orders like every primitive; the
        // batch merges runs of paths with no smoothing/texture split.
        let mut scene = new_scene();
        for _ in 0..3 {
            scene
                .insert_path(path_record(&full_bounds(), 3), triangle_vertices(0.0, 0.0))
                .unwrap();
        }
        scene.finish().unwrap();
        assert_eq!(scene.paths.iter().map(|p| p.rec.order).collect::<Vec<_>>(), vec![1, 2, 3]);
        assert_eq!(scene.plan.commands.len(), 1);
        let command = &scene.plan.commands[0];
        assert_eq!(command.primitive_kind, batch_primitive_kind::PATHS);
        assert_eq!(command.range_start, 0);
        assert_eq!(command.range_end, 3);
        assert_eq!(command.rasterization_vertex_count, 9);
        // All three share the first's order? No: overlapping bounds step
        // orders 1..3, so the sprite count is the combined form.
        assert_eq!(command.sprite_count, 1);
        let requirements = &scene.plan.requirements;
        assert_eq!(requirements.path_rasterization_vertex_count, 9);
        assert_eq!(requirements.path_sprite_count, 1);
        assert_eq!(requirements.uses_path_target, 1);
        assert_eq!(requirements.instance_batch_count, 2);
    }

    #[test]
    fn same_order_paths_count_one_sprite_each() {
        // Non-overlapping paths reuse order 1 (the pinned sprite-count
        // rule: paths.len() when the last shares the first's order).
        let mut scene = new_scene();
        let at = |x: f32| GpuiGoSceneBounds::from_f32(F32Bounds {
            origin_x: x,
            origin_y: 0.0,
            width: 10.0,
            height: 10.0,
        });
        for x in [0.0, 20.0, 40.0] {
            scene.insert_path(path_record(&at(x), 3), triangle_vertices(x, 0.0)).unwrap();
        }
        scene.finish().unwrap();
        assert_eq!(scene.paths.iter().map(|p| p.rec.order).collect::<Vec<_>>(), vec![1, 1, 1]);
        let command = &scene.plan.commands[0];
        assert_eq!(command.sprite_count, 3);
        assert_eq!(scene.plan.requirements.path_sprite_count, 3);
    }

    #[test]
    fn empty_clipped_paths_drop_and_empty_vertex_paths_keep_their_batch() {
        let mut scene = new_scene();
        // A path fully outside the mask drops (no array entry, no op).
        let outside = GpuiGoSceneBounds::from_f32(F32Bounds {
            origin_x: 500.0,
            origin_y: 500.0,
            width: 10.0,
            height: 10.0,
        });
        scene.insert_path(path_record(&outside, 3), triangle_vertices(500.0, 500.0)).unwrap();
        assert_eq!(scene.paths.len(), 0);
        assert_eq!(scene.ops.len(), 0);

        // A zero-vertex path inserts and batches with
        // rasterization_vertex_count 0: the draw path skips it, and the
        // requirements do not turn on the path target.
        scene.insert_path(path_record(&full_bounds(), 0), Vec::new()).unwrap();
        scene.finish().unwrap();
        assert_eq!(scene.paths.len(), 1);
        let command = &scene.plan.commands[0];
        assert_eq!(command.primitive_kind, batch_primitive_kind::PATHS);
        assert_eq!(command.rasterization_vertex_count, 0);
        assert_eq!(command.sprite_count, 1);
        assert_eq!(scene.plan.requirements.uses_path_target, 0);
        assert_eq!(scene.plan.requirements.path_sprite_count, 0);
        assert_eq!(scene.plan.requirements.instance_batch_count, 0);
    }

    #[test]
    fn replay_reinserts_paths_with_recomputed_orders() {
        let mut source = new_scene();
        source
            .insert_path(path_record(&full_bounds(), 3), triangle_vertices(0.0, 0.0))
            .unwrap();
        source.finish().unwrap();
        let ops = source.ops.clone();

        let mut target = new_scene();
        target.insert_quad(detached_quad()).unwrap(); // order 1 in the target
        target.replay(0, ops.len(), ops).unwrap();
        target.finish().unwrap();
        // The replayed path overlaps nothing in the target: order 1,
        // sharing the batch stream with the detached quad.
        assert_eq!(target.paths[0].rec.order, 1);
        assert_eq!(target.quads[0].order, 1);
        // PrimitiveKind discriminants: Path(3) after Quad(2).
        assert_eq!(target.plan.commands[0].primitive_kind, batch_primitive_kind::QUADS);
        assert_eq!(target.plan.commands[1].primitive_kind, batch_primitive_kind::PATHS);
    }

    #[test]
    fn path_service_insert_validates_records() {
        let mut handle = 0u64;
        assert_eq!(scene_create_body(&mut handle), scene_status::OK);
        // Non-zero order.
        let mut bad = path_record(&full_bounds(), 0);
        bad.order = 3;
        assert_eq!(
            scene_insert_path_body(handle, &bad, core::ptr::null(), 0),
            scene_status::ERR_BAD_VALUE
        );
        // Vertex-count disagreement.
        let bad = path_record(&full_bounds(), 3);
        assert_eq!(
            scene_insert_path_body(handle, &bad, core::ptr::null(), 0),
            scene_status::ERR_BAD_VALUE
        );
        // A valid insert, then the dump path: before finish, capacity,
        // then content.
        let rec = path_record(&full_bounds(), 3);
        let vertices = triangle_vertices(0.0, 0.0);
        assert_eq!(
            scene_insert_path_body(handle, &rec, vertices.as_ptr(), 3),
            scene_status::OK
        );
        let mut out_records = 0u32;
        let mut out_vertices = 0u32;
        let mut record_buffer = [GpuiGoScenePathRecord::default()];
        let mut vertex_buffer = [GpuiGoScenePathVertexRecord::default(); 4];
        assert_eq!(
            scene_path_dump_body(
                handle,
                record_buffer.as_mut_ptr(),
                vertex_buffer.as_mut_ptr(),
                1,
                4,
                &mut out_records,
                &mut out_vertices,
            ),
            scene_status::ERR_NOT_FINISHED
        );
        assert_eq!(scene_finish_body(handle), scene_status::OK);
        // Vertex capacity too small.
        assert_eq!(
            scene_path_dump_body(
                handle,
                record_buffer.as_mut_ptr(),
                core::ptr::null_mut(),
                1,
                0,
                &mut out_records,
                &mut out_vertices,
            ),
            scene_status::ERR_CAPACITY
        );
        assert_eq!(out_vertices, 3);
        assert_eq!(
            scene_path_dump_body(
                handle,
                record_buffer.as_mut_ptr(),
                vertex_buffer.as_mut_ptr(),
                1,
                4,
                &mut out_records,
                &mut out_vertices,
            ),
            scene_status::OK
        );
        assert_eq!(out_records, 1);
        assert_eq!(out_vertices, 3);
        assert_eq!(record_buffer[0].order, 1);
        assert_eq!(record_buffer[0].vertex_count, 3);
        assert_eq!(f32::from_bits(vertex_buffer[0].xy_x), 0.0);
        assert_eq!(f32::from_bits(vertex_buffer[1].xy_x), 10.0);
        assert_eq!(f32::from_bits(vertex_buffer[0].st_v), 1.0);
        assert_eq!(scene_dispose_body(handle), scene_status::OK);
    }

    #[test]
    fn path_script_tessellates_through_the_pinned_builder() {
        // A filled square (move, line, line, line, close) tessellates
        // into two triangles = six vertices, with st (0, 1) per vertex
        // and bounds spanning the square.
        let commands = [
            GpuiGoPathCommandRecord {
                kind: path_command_kind::MOVE_TO,
                data: [0.0f32.to_bits(), 4.0f32.to_bits(), 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
                ..Default::default()
            },
            GpuiGoPathCommandRecord {
                kind: path_command_kind::LINE_TO,
                data: [30.0f32.to_bits(), 4.0f32.to_bits(), 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
                ..Default::default()
            },
            GpuiGoPathCommandRecord {
                kind: path_command_kind::LINE_TO,
                data: [30.0f32.to_bits(), 28.0f32.to_bits(), 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
                ..Default::default()
            },
            GpuiGoPathCommandRecord {
                kind: path_command_kind::LINE_TO,
                data: [0.0f32.to_bits(), 28.0f32.to_bits(), 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
                ..Default::default()
            },
            GpuiGoPathCommandRecord {
                kind: path_command_kind::CLOSE,
                data: [0; 12],
                ..Default::default()
            },
        ];
        let mut out_vertices = [GpuiGoScenePathVertexRecord::default(); 16];
        let mut out_count = 0u32;
        let mut out_bounds = GpuiGoSceneBounds::default();
        assert_eq!(
            scene_path_script_body(
                commands.as_ptr(),
                commands.len() as u32,
                core::ptr::null(),
                0,
                out_vertices.as_mut_ptr(),
                16,
                &mut out_count,
                &mut out_bounds,
            ),
            scene_status::OK
        );
        assert_eq!(out_count, 6, "a closed square fills as two triangles");
        assert_eq!(f32::from_bits(out_bounds.x), 0.0);
        assert_eq!(f32::from_bits(out_bounds.y), 4.0);
        assert_eq!(f32::from_bits(out_bounds.w), 30.0);
        assert_eq!(f32::from_bits(out_bounds.h), 24.0);
        for vertex in &out_vertices[..out_count as usize] {
            assert_eq!(f32::from_bits(vertex.st_u), 0.0);
            assert_eq!(f32::from_bits(vertex.st_v), 1.0);
        }

        // Capacity retry reports the required count.
        assert_eq!(
            scene_path_script_body(
                commands.as_ptr(),
                commands.len() as u32,
                core::ptr::null(),
                0,
                core::ptr::null_mut(),
                0,
                &mut out_count,
                &mut out_bounds,
            ),
            scene_status::ERR_CAPACITY
        );
        assert_eq!(out_count, 6);

        // A stroke tessellates with width (butt caps, miter joins):
        // more vertices than the fill.
        let stroke_commands = [
            GpuiGoPathCommandRecord {
                kind: path_command_kind::STYLE,
                data: [
                    path_style_tag::STROKE,
                    3.0f32.to_bits(),
                    0,
                    0,
                    0,
                    4.0f32.to_bits(),
                    0,
                    0,
                    0,
                    0,
                    0,
                    0,
                ],
                ..Default::default()
            },
            GpuiGoPathCommandRecord {
                kind: path_command_kind::MOVE_TO,
                data: [0.0f32.to_bits(), 4.0f32.to_bits(), 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
                ..Default::default()
            },
            GpuiGoPathCommandRecord {
                kind: path_command_kind::LINE_TO,
                data: [30.0f32.to_bits(), 4.0f32.to_bits(), 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
                ..Default::default()
            },
        ];
        let mut out_count = 0u32;
        assert_eq!(
            scene_path_script_body(
                stroke_commands.as_ptr(),
                stroke_commands.len() as u32,
                core::ptr::null(),
                0,
                out_vertices.as_mut_ptr(),
                16,
                &mut out_count,
                &mut out_bounds,
            ),
            scene_status::OK
        );
        assert!(out_count > 0);

        // Validation: unknown kind, bad bool, out-of-range pool index.
        let mut bad = commands[0];
        bad.kind = 99;
        assert_eq!(
            scene_path_script_body(
                &bad,
                1,
                core::ptr::null(),
                0,
                out_vertices.as_mut_ptr(),
                16,
                &mut out_count,
                &mut out_bounds,
            ),
            scene_status::ERR_BAD_VALUE
        );
        let mut bad = commands[0];
        bad.data[0] = 0x7F80_0000; // +inf x
        assert_eq!(
            scene_path_script_body(
                &bad,
                1,
                core::ptr::null(),
                0,
                out_vertices.as_mut_ptr(),
                16,
                &mut out_count,
                &mut out_bounds,
            ),
            scene_status::ERR_BAD_VALUE
        );
        let polygon = GpuiGoPathCommandRecord {
            kind: path_command_kind::POLYGON,
            data: [0, 5, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0],
            ..Default::default()
        };
        assert_eq!(
            scene_path_script_body(
                &polygon,
                1,
                core::ptr::null(),
                0,
                out_vertices.as_mut_ptr(),
                16,
                &mut out_count,
                &mut out_bounds,
            ),
            scene_status::ERR_BAD_VALUE
        );
    }

    #[test]
    fn path_script_round_cap_and_quadratic_curve_tessellate() {
        // Round start cap: the stroke builder emits cap geometry.
        let commands = [
            GpuiGoPathCommandRecord {
                kind: path_command_kind::STYLE,
                data: [
                    path_style_tag::STROKE,
                    4.0f32.to_bits(),
                    2, // start cap: round
                    0,
                    1, // line join: round
                    4.0f32.to_bits(),
                    0,
                    0,
                    0,
                    0,
                    0,
                    0,
                ],
                ..Default::default()
            },
            GpuiGoPathCommandRecord {
                kind: path_command_kind::MOVE_TO,
                data: [2.0f32.to_bits(), 2.0f32.to_bits(), 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
                ..Default::default()
            },
            GpuiGoPathCommandRecord {
                kind: path_command_kind::LINE_TO,
                data: [28.0f32.to_bits(), 2.0f32.to_bits(), 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
                ..Default::default()
            },
        ];
        let mut out_vertices = [GpuiGoScenePathVertexRecord::default(); 64];
        let mut out_count = 0u32;
        let mut out_bounds = GpuiGoSceneBounds::default();
        assert_eq!(
            scene_path_script_body(
                commands.as_ptr(),
                commands.len() as u32,
                core::ptr::null(),
                0,
                out_vertices.as_mut_ptr(),
                64,
                &mut out_count,
                &mut out_bounds,
            ),
            scene_status::OK
        );
        assert!(out_count > 2);

        // A quadratic curve flattens into multiple triangles.
        let curve = [
            GpuiGoPathCommandRecord {
                kind: path_command_kind::MOVE_TO,
                data: [0.0f32.to_bits(), 0.0f32.to_bits(), 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
                ..Default::default()
            },
            GpuiGoPathCommandRecord {
                kind: path_command_kind::CURVE_TO,
                // to (20, 0), ctrl (10, 20)
                data: [
                    20.0f32.to_bits(),
                    0.0f32.to_bits(),
                    10.0f32.to_bits(),
                    20.0f32.to_bits(),
                    0,
                    0,
                    0,
                    0,
                    0,
                    0,
                    0,
                    0,
                ],
                ..Default::default()
            },
        ];
        let mut out_count = 0u32;
        assert_eq!(
            scene_path_script_body(
                curve.as_ptr(),
                curve.len() as u32,
                core::ptr::null(),
                0,
                out_vertices.as_mut_ptr(),
                64,
                &mut out_count,
                &mut out_bounds,
            ),
            scene_status::OK
        );
        assert!(out_count >= 3, "a quadratic fills at least one triangle");
    }
}
