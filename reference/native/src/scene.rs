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
//!   emits content-filter boundaries one at a time, pairs nested filter
//!   groups (`MAX_FILTER_GROUP_DEPTH` = 2: the first two nested groups
//!   get dedicated offscreen targets, deeper groups render inline) and
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
pub const GPUI_GO_SCENE_SERVICE_VERSION: u32 = 3;

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
/// Maximum filters in one filter chain (the pinned `SmallVec<[ScaledFilter; 4]>`
/// inline capacity).
pub const MAX_SCENE_FILTERS: u32 = 4;
/// Maximum compiled plan commands in one scene.
pub const MAX_SCENE_PLAN_COMMANDS: u32 = 262_144;

/// Maximum vertices in one inserted path (an ABI memory bound on the
/// bulk vertex array of `insert_path`; the pinned scene imposes no
/// explicit per-path bound, but the ABI validates every bulk copy).
pub const MAX_SCENE_PATH_VERTICES: u32 = 1_048_576;

/// Nested content-filter groups with dedicated isolation targets; deeper
/// groups render inline (pinned `plan.rs::MAX_FILTER_GROUP_DEPTH`).
pub const MAX_FILTER_GROUP_DEPTH: usize = 2;

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

/// Quad record. Layout (x86-64, `#[repr(C)]`): `order` @0, `bounds` @4,
/// `content_mask` @20, `background` @36, `border_color` @56,
/// `corner_radii` @76, `border_widths` @92, `border_style` @108,
/// `border_dashed_length` @112, `border_dashed_gap` @116,
/// `corner_smoothing` @120, `padding` @124, `record_size` @128;
/// size 132, alignment 4.
///
/// `order` mirrors the pinned `Quad::order`: it must be 0 on insert (the
/// kernel assigns the draw order) and carries the assigned order in
/// dumps.
#[repr(C)]
#[derive(Clone, Copy, PartialEq, Debug)]
pub struct GpuiGoSceneQuadRecord {
    /// Draw order. Must be 0 on insert; kernel-assigned in dumps.
    pub order: u32,
    /// Bounds in device pixels.
    pub bounds: GpuiGoSceneBounds,
    /// Content mask bounds in device pixels (the clip).
    pub content_mask: GpuiGoSceneBounds,
    /// Background color.
    pub background: GpuiGoSceneColor,
    /// Border color.
    pub border_color: GpuiGoSceneColor,
    /// Corner radii (top-left, top-right, bottom-right, bottom-left), f32
    /// bits, device pixels.
    pub corner_radii: [u32; 4],
    /// Border widths (top, right, bottom, left), f32 bits, device pixels.
    pub border_widths: [u32; 4],
    /// 0 = solid, 1 = dashed (pinned `BorderStyle`).
    pub border_style: u32,
    /// Dashed border dash length, f32 bits (multiple of border width).
    pub border_dashed_length: u32,
    /// Dashed border dash gap, f32 bits.
    pub border_dashed_gap: u32,
    /// Corner smoothing, f32 bits.
    pub corner_smoothing: u32,
    /// Padding (payload; the pinned field, always 0 from the paint path).
    pub padding: u32,
    /// `size_of::<GpuiGoSceneQuadRecord>()` self-check.
    pub record_size: u32,
}

impl Default for GpuiGoSceneQuadRecord {
    fn default() -> Self {
        GpuiGoSceneQuadRecord {
            order: 0,
            bounds: GpuiGoSceneBounds::default(),
            content_mask: GpuiGoSceneBounds::default(),
            background: GpuiGoSceneColor::default(),
            border_color: GpuiGoSceneColor::default(),
            corner_radii: [0; 4],
            border_widths: [0; 4],
            border_style: 0,
            border_dashed_length: 0,
            border_dashed_gap: 0,
            corner_smoothing: 0,
            padding: 0,
            record_size: core::mem::size_of::<GpuiGoSceneQuadRecord>() as u32,
        }
    }
}

/// Shadow record. Layout: `order` @0, `blur_radius` @4, `bounds` @8,
/// `corner_radii` @24, `content_mask` @40, `color` @56, `element_bounds`
/// @76, `element_corner_radii` @92, `inset` @108, `corner_smoothing`
/// @112, `record_size` @116; size 120, alignment 4.
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
    pub content_mask: GpuiGoSceneBounds,
    /// Shadow color.
    pub color: GpuiGoSceneColor,
    /// The element's bounds (the shadow's un-dilated frame).
    pub element_bounds: GpuiGoSceneBounds,
    /// The element's corner radii, f32 bits.
    pub element_corner_radii: [u32; 4],
    /// 0 = drop shadow, 1 = inset (pinned `ShaderBool`).
    pub inset: u32,
    /// Corner smoothing, f32 bits.
    pub corner_smoothing: u32,
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
            content_mask: GpuiGoSceneBounds::default(),
            color: GpuiGoSceneColor::default(),
            element_bounds: GpuiGoSceneBounds::default(),
            element_corner_radii: [0; 4],
            inset: 0,
            corner_smoothing: 0,
            record_size: core::mem::size_of::<GpuiGoSceneShadowRecord>() as u32,
        }
    }
}

/// Underline record. Layout: `order` @0, `padding` @4, `bounds` @8,
/// `content_mask` @24, `color` @40, `thickness` @60, `wavy` @64,
/// `record_size` @68; size 72, alignment 4.
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
    pub content_mask: GpuiGoSceneBounds,
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
            content_mask: GpuiGoSceneBounds::default(),
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
/// `content_mask` @20, `corner_radii` @36, `corner_smoothing` @52,
/// `blur_radii` @56, `filter_count` @72, `opacity` @76, `is_start` @80,
/// `record_size` @84; size 88, alignment 4.
///
/// Filters are gaussian blurs with radii in device pixels (the pinned
/// `ScaledFilter::Blur`); up to [`MAX_SCENE_FILTERS`] entries with unused
/// entries zero.
#[repr(C)]
#[derive(Clone, Copy, PartialEq, Debug)]
pub struct GpuiGoSceneFilterRecord {
    /// Draw order. Must be 0 on insert; kernel-assigned in dumps.
    pub order: u32,
    /// Bounds in device pixels.
    pub bounds: GpuiGoSceneBounds,
    /// Content mask bounds.
    pub content_mask: GpuiGoSceneBounds,
    /// Corner radii, f32 bits.
    pub corner_radii: [u32; 4],
    /// Corner smoothing, f32 bits.
    pub corner_smoothing: u32,
    /// Blur radii (device pixels, f32 bits), first `filter_count` entries.
    pub blur_radii: [u32; 4],
    /// Number of filters in the chain, 0..=4.
    pub filter_count: u32,
    /// Group opacity, f32 bits (element opacity captured at paint time).
    pub opacity: u32,
    /// Boundary marker: 1 = start (opens the group), 0 = end. Backdrop
    /// inserts pass 0.
    pub is_start: u32,
    /// `size_of::<GpuiGoSceneFilterRecord>()` self-check.
    pub record_size: u32,
}

impl Default for GpuiGoSceneFilterRecord {
    fn default() -> Self {
        GpuiGoSceneFilterRecord {
            order: 0,
            bounds: GpuiGoSceneBounds::default(),
            content_mask: GpuiGoSceneBounds::default(),
            corner_radii: [0; 4],
            corner_smoothing: 0,
            blur_radii: [0; 4],
            filter_count: 0,
            opacity: 0,
            is_start: 0,
            record_size: core::mem::size_of::<GpuiGoSceneFilterRecord>() as u32,
        }
    }
}

/// Surface record. Layout: `order` @0, `bounds` @4, `content_mask` @20,
/// `source_tag` @36, `source_reserved` @40, `record_size` @56;
/// size 56, alignment 4.
///
/// The source is opaque payload (tag 0 = the pinned
/// `SurfaceSource::Unsupported` stand-in used by tests); the paired
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
    pub content_mask: GpuiGoSceneBounds,
    /// Source kind tag (payload; 0 = none/unsupported).
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
            content_mask: GpuiGoSceneBounds::default(),
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
/// @4, `bounds` @8, `content_mask` @24, `color` @40, `tile` @60,
/// `transformation` @92 (rotation-scale 2x2 then translation xy, f32
/// bits), `record_size` @116; size 120, alignment 4.
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
    pub content_mask: GpuiGoSceneBounds,
    /// The mask color (monochrome coverage tint / subpixel blend color).
    pub color: GpuiGoSceneColor,
    /// The atlas tile this sprite samples.
    pub tile: GpuiGoSceneTileRecord,
    /// The pinned `TransformationMatrix` fields as f32 bits:
    /// rotation_scale[0][0], [0][1], [1][0], [1][1], translation[0],
    /// translation[1].
    pub transformation: [u32; 6],
    /// `size_of::<GpuiGoSceneSpriteRecord>()` self-check.
    pub record_size: u32,
}

impl Default for GpuiGoSceneSpriteRecord {
    fn default() -> Self {
        GpuiGoSceneSpriteRecord {
            order: 0,
            padding: 0,
            bounds: GpuiGoSceneBounds::default(),
            content_mask: GpuiGoSceneBounds::default(),
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
            record_size: core::mem::size_of::<GpuiGoSceneSpriteRecord>() as u32,
        }
    }
}

/// Polychrome sprite record (the pinned `PolychromeSprite`,
/// `crates/gpui/src/scene.rs:1107-1121`: no color, no transformation;
/// grayscale flag, opacity, corner radii and smoothing instead).
/// Layout: `order` @0, `grayscale` @4, `opacity` @8, `corner_smoothing`
/// @12, `bounds` @16, `content_mask` @32, `corner_radii` @48, `tile`
/// @64, `record_size` @96; size 100, alignment 4.
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
    pub content_mask: GpuiGoSceneBounds,
    /// Corner radii, f32 bits, device pixels.
    pub corner_radii: [u32; 4],
    /// The atlas tile this sprite samples.
    pub tile: GpuiGoSceneTileRecord,
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
            content_mask: GpuiGoSceneBounds::default(),
            corner_radii: [0; 4],
            tile: GpuiGoSceneTileRecord::default(),
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
/// `content_mask` @20, `color` @36, `vertex_count` @56,
/// `record_size` @60; size 64, alignment 4.
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
    pub content_mask: GpuiGoSceneBounds,
    /// Path fill color.
    pub color: GpuiGoSceneColor,
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
            content_mask: GpuiGoSceneBounds::default(),
            color: GpuiGoSceneColor::default(),
            vertex_count: 0,
            record_size: core::mem::size_of::<GpuiGoScenePathRecord>() as u32,
        }
    }
}

/// One compiled render-plan command. Layout: `command_kind` @0,
/// `primitive_kind` @4, `range_start` @8, `range_end` @12, `smoothed` @16,
/// `texture_index` @20, `rasterization_vertex_count` @24,
/// `sprite_count` @28, `boundary_index` @32, `closing_boundary_index`
/// @36, `filter_target` @40, `target_index` @44, `record_size` @48;
/// size 52, alignment 4.
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
    /// 1 after `scene_finish`.
    pub is_finished: u32,
    pub record_size: u32,
}

// Compile-time layout pins; the Go loader mirrors these exactly.
const _: () = assert!(core::mem::size_of::<GpuiGoSceneBounds>() == 16);
const _: () = assert!(core::mem::align_of::<GpuiGoSceneBounds>() == 4);
const _: () = assert!(core::mem::size_of::<GpuiGoSceneColor>() == 20);
const _: () = assert!(core::mem::align_of::<GpuiGoSceneColor>() == 4);
const _: () = assert!(core::mem::size_of::<GpuiGoSceneQuadRecord>() == 132);
const _: () = assert!(core::mem::align_of::<GpuiGoSceneQuadRecord>() == 4);
const _: () = assert!(core::mem::size_of::<GpuiGoSceneShadowRecord>() == 120);
const _: () = assert!(core::mem::align_of::<GpuiGoSceneShadowRecord>() == 4);
const _: () = assert!(core::mem::size_of::<GpuiGoSceneUnderlineRecord>() == 72);
const _: () = assert!(core::mem::align_of::<GpuiGoSceneUnderlineRecord>() == 4);
const _: () = assert!(core::mem::size_of::<GpuiGoSceneFilterRecord>() == 88);
const _: () = assert!(core::mem::align_of::<GpuiGoSceneFilterRecord>() == 4);
const _: () = assert!(core::mem::size_of::<GpuiGoSceneSurfaceRecord>() == 56);
const _: () = assert!(core::mem::align_of::<GpuiGoSceneSurfaceRecord>() == 4);
const _: () = assert!(core::mem::size_of::<GpuiGoSceneTileRecord>() == 32);
const _: () = assert!(core::mem::align_of::<GpuiGoSceneTileRecord>() == 4);
const _: () = assert!(core::mem::size_of::<GpuiGoSceneSpriteRecord>() == 120);
const _: () = assert!(core::mem::align_of::<GpuiGoSceneSpriteRecord>() == 4);
const _: () = assert!(core::mem::size_of::<GpuiGoScenePolychromeSpriteRecord>() == 100);
const _: () = assert!(core::mem::align_of::<GpuiGoScenePolychromeSpriteRecord>() == 4);
const _: () = assert!(core::mem::size_of::<GpuiGoSceneCommandRecord>() == 52);
const _: () = assert!(core::mem::align_of::<GpuiGoSceneCommandRecord>() == 4);
const _: () = assert!(core::mem::size_of::<GpuiGoSceneRequirementsRecord>() == 44);
const _: () = assert!(core::mem::align_of::<GpuiGoSceneRequirementsRecord>() == 4);
const _: () = assert!(core::mem::size_of::<GpuiGoSceneMetaRecord>() == 56);
const _: () = assert!(core::mem::align_of::<GpuiGoSceneMetaRecord>() == 4);
const _: () = assert!(core::mem::size_of::<GpuiGoScenePathRecord>() == 64);
const _: () = assert!(core::mem::align_of::<GpuiGoScenePathRecord>() == 4);
const _: () = assert!(core::mem::size_of::<GpuiGoScenePathVertexRecord>() == 16);
const _: () = assert!(core::mem::align_of::<GpuiGoScenePathVertexRecord>() == 4);
const _: () = assert!(core::mem::size_of::<GpuiGoPathCommandRecord>() == 56);
const _: () = assert!(core::mem::align_of::<GpuiGoPathCommandRecord>() == 4);
const _: () = assert!(core::mem::size_of::<GpuiGoSceneTable>() == 280);
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
    StartLayer(F32Bounds),
    EndLayer,
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
    plan: ScenePlanState,
    layer_push_count: u32,
    is_finished: bool,
    failed: bool,
}

/// The compiled plan (the pinned `ScenePlan`).
#[derive(Debug, Default, Clone)]
struct ScenePlanState {
    commands: Vec<GpuiGoSceneCommandRecord>,
    requirements: GpuiGoSceneRequirementsRecord,
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
        self.plan.commands.clear();
        self.plan.requirements = GpuiGoSceneRequirementsRecord::default();
        self.layer_push_count = 0;
        self.is_finished = false;
        self.failed = false;
    }

    /// The clipped bounds of a primitive record: bounds ∩ content mask,
    /// with the pinned `Bounds::intersect` arithmetic.
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
        is_filter_boundary: bool,
    ) -> Result<Option<u32>, i32> {
        let clipped_bounds = Self::clipped_bounds(bounds, mask);
        let clipped_is_empty =
            clipped_bounds.width <= 0.0 || clipped_bounds.height <= 0.0;

        if clipped_is_empty && !is_filter_boundary {
            return Ok(None);
        }

        let order = if is_filter_boundary {
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
        let Some(order) = self.insert_common(&rec.bounds, &rec.content_mask, false)? else {
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
        let Some(order) = self.insert_common(&rec.bounds, &rec.content_mask, false)? else {
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
        let Some(order) = self.insert_common(&rec.bounds, &rec.content_mask, false)? else {
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
        let Some(order) = self.insert_common(&rec.bounds, &rec.content_mask, false)? else {
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
        // Boundaries are never dropped (matched pairs must survive
        // clipping), so insert_common always yields an order here.
        let order = self
            .insert_common(&rec.bounds, &rec.content_mask, true)?
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
        self.boundaries.push(rec);
        self.ops.push(PaintOp::Boundary(rec));
        Ok(())
    }

    fn insert_surface(
        &mut self,
        mut rec: GpuiGoSceneSurfaceRecord,
        opacity: f32,
    ) -> Result<(), i32> {
        self.check_insert_capacity(self.surfaces.len())?;
        let Some(order) = self.insert_common(&rec.bounds, &rec.content_mask, false)? else {
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
        let Some(order) = self.insert_common(&rec.bounds, &rec.content_mask, false)? else {
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
        let Some(order) = self.insert_common(&rec.bounds, &rec.content_mask, false)? else {
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
        let Some(order) = self.insert_common(&rec.bounds, &rec.content_mask, false)? else {
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
        let mut requirements = GpuiGoSceneRequirementsRecord {
            record_size: core::mem::size_of::<GpuiGoSceneRequirementsRecord>() as u32,
            ..Default::default()
        };

        // Matched starts: an end marker pops the most recent unmatched
        // start (pinned pending_starts).
        let mut matched_starts = vec![false; self.boundaries.len()];
        let mut pending_starts: Vec<usize> = Vec::new();
        for (index, boundary) in self.boundaries.iter().enumerate() {
            if boundary.is_start != 0 {
                pending_starts.push(index);
            } else if let Some(start_index) = pending_starts.pop() {
                matched_starts[start_index] = true;
            }
        }

        let mut filter_stack: Vec<(usize, FilterRenderTarget)> = Vec::new();
        let mut isolated_depth: usize = 0;

        for batch in BatchIterator::new(self) {
            match batch {
                PrimitiveBatch::FilterBoundary(boundary_index) => {
                    let boundary = &self.boundaries[boundary_index];
                    if boundary.is_start != 0 {
                        let target = if matched_starts[boundary_index]
                            && isolated_depth < MAX_FILTER_GROUP_DEPTH
                        {
                            FilterRenderTarget::Isolated(isolated_depth)
                        } else {
                            FilterRenderTarget::Inline
                        };
                        if target.is_isolated() {
                            isolated_depth += 1;
                            requirements.uses_offscreen_target = 1;
                            requirements.isolated_target_count =
                                requirements.isolated_target_count.max(isolated_depth as u32);
                        }
                        filter_stack.push((boundary_index, target));
                        commands.push(command_record(
                            command_kind::BEGIN_FILTER,
                            boundary_index,
                            boundary_index,
                            target,
                        ));
                    } else if let Some((start_index, target)) = filter_stack.pop() {
                        if target.is_isolated() {
                            isolated_depth -= 1;
                            requirements.isolated_filter_count += 1;
                        }
                        commands.push(command_record(
                            command_kind::END_FILTER,
                            start_index,
                            boundary_index,
                            target,
                        ));
                    } else {
                        // Unmatched end: the pinned plan emits an inline end
                        // command (debug_assert in debug builds).
                        commands.push(command_record(
                            command_kind::END_FILTER,
                            boundary_index,
                            boundary_index,
                            FilterRenderTarget::Inline,
                        ));
                    }
                }
                batch => {
                    include_batch(&mut requirements, &batch);
                    commands.push(batch_command_record(&batch));
                }
            }
            if commands.len() > MAX_SCENE_PLAN_COMMANDS as usize {
                return Err(scene_status::ERR_LIMIT);
            }
        }

        requirements.command_count = commands.len() as u32;
        self.plan.commands = commands;
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
    Shadow,
    Quad,
    Path,
    Underline,
    MonochromeSprite,
    SubpixelSprite,
    PolychromeSprite,
    Surface,
    BackdropFilter,
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
    MonochromeSprites { texture_index: u32, range: std::ops::Range<usize> },
    SubpixelSprites { texture_index: u32, range: std::ops::Range<usize> },
    PolychromeSprites { texture_index: u32, range: std::ops::Range<usize>, smoothed: bool },
    Surfaces(std::ops::Range<usize>),
    BackdropFilters(std::ops::Range<usize>),
    FilterBoundary(usize),
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
        PrimitiveBatch::MonochromeSprites { texture_index, range } => {
            rec.primitive_kind = batch_primitive_kind::MONOCHROME_SPRITES;
            rec.range_start = range.start as u32;
            rec.range_end = range.end as u32;
            // The batch's atlas texture (the pinned
            // `PrimitiveBatch::MonochromeSprites { texture_id, range }`;
            // the kind pool is implied by the primitive kind).
            rec.texture_index = *texture_index;
        }
        PrimitiveBatch::SubpixelSprites { texture_index, range } => {
            rec.primitive_kind = batch_primitive_kind::SUBPIXEL_SPRITES;
            rec.range_start = range.start as u32;
            rec.range_end = range.end as u32;
            rec.texture_index = *texture_index;
        }
        PrimitiveBatch::PolychromeSprites { texture_index, range, smoothed } => {
            rec.primitive_kind = batch_primitive_kind::POLYCHROME_SPRITES;
            rec.range_start = range.start as u32;
            rec.range_end = range.end as u32;
            rec.texture_index = *texture_index;
            rec.smoothed = u32::from(*smoothed);
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
            // (the pinned `next_if` condition).
            PrimitiveKind::MonochromeSprite => {
                let texture_index = self.scene.monochrome_sprites[self.monochrome_sprites_cursor].tile.texture_index;
                let start = self.monochrome_sprites_start;
                let mut end = start + 1;
                self.monochrome_sprites_cursor += 1;
                while let Some(sprite) = self.scene.monochrome_sprites.get(self.monochrome_sprites_cursor) {
                    if precedes_limit(sprite.order, batch_kind, max_order_and_kind)
                        && sprite.tile.texture_index == texture_index
                    {
                        self.monochrome_sprites_cursor += 1;
                        end += 1;
                    } else {
                        break;
                    }
                }
                self.monochrome_sprites_start = end;
                Some(PrimitiveBatch::MonochromeSprites { texture_index, range: start..end })
            }
            PrimitiveKind::SubpixelSprite => {
                let texture_index = self.scene.subpixel_sprites[self.subpixel_sprites_cursor].tile.texture_index;
                let start = self.subpixel_sprites_start;
                let mut end = start + 1;
                self.subpixel_sprites_cursor += 1;
                while let Some(sprite) = self.scene.subpixel_sprites.get(self.subpixel_sprites_cursor) {
                    if precedes_limit(sprite.order, batch_kind, max_order_and_kind)
                        && sprite.tile.texture_index == texture_index
                    {
                        self.subpixel_sprites_cursor += 1;
                        end += 1;
                    } else {
                        break;
                    }
                }
                self.subpixel_sprites_start = end;
                Some(PrimitiveBatch::SubpixelSprites { texture_index, range: start..end })
            }
            PrimitiveKind::PolychromeSprite => {
                let first = &self.scene.polychrome_sprites[self.polychrome_sprites_cursor];
                let texture_index = first.tile.texture_index;
                let smoothed = has_corner_smoothing(first.corner_smoothing);
                let start = self.polychrome_sprites_start;
                let mut end = start + 1;
                self.polychrome_sprites_cursor += 1;
                while let Some(sprite) = self.scene.polychrome_sprites.get(self.polychrome_sprites_cursor) {
                    if precedes_limit(sprite.order, batch_kind, max_order_and_kind)
                        && sprite.tile.texture_index == texture_index
                        && has_corner_smoothing(sprite.corner_smoothing) == smoothed
                    {
                        self.polychrome_sprites_cursor += 1;
                        end += 1;
                    } else {
                        break;
                    }
                }
                self.polychrome_sprites_start = end;
                Some(PrimitiveBatch::PolychromeSprites { texture_index, range: start..end, smoothed })
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

fn check_bool01(value: u32) -> Result<(), i32> {
    if value > 1 {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    Ok(())
}

fn check_quad(rec: &GpuiGoSceneQuadRecord) -> Result<(), i32> {
    check_record_size(rec.record_size, core::mem::size_of::<GpuiGoSceneQuadRecord>() as u32)?;
    if rec.order != 0 {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    if rec.border_style > 1 {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    check_color(&rec.background)?;
    check_color(&rec.border_color)?;
    Ok(())
}

fn check_shadow(rec: &GpuiGoSceneShadowRecord) -> Result<(), i32> {
    check_record_size(rec.record_size, core::mem::size_of::<GpuiGoSceneShadowRecord>() as u32)?;
    if rec.order != 0 {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    check_bool01(rec.inset)?;
    check_color(&rec.color)?;
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
    Ok(())
}

fn check_surface(rec: &GpuiGoSceneSurfaceRecord) -> Result<(), i32> {
    check_record_size(rec.record_size, core::mem::size_of::<GpuiGoSceneSurfaceRecord>() as u32)?;
    if rec.order != 0 {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    if rec.source_reserved != [0u32; 3] {
        return Err(scene_status::ERR_BAD_VALUE);
    }
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
    check_tile(&rec.tile, class)
}

/// Validates a polychrome sprite record.
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
    check_tile(&rec.tile, SpriteClass::Polychrome)
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
    check_color(&rec.color)?;
    if rec.vertex_count != vertex_count {
        return Err(scene_status::ERR_BAD_VALUE);
    }
    if vertex_count > MAX_SCENE_PATH_VERTICES {
        return Err(scene_status::ERR_BAD_VALUE);
    }
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
    pub quads: Vec<gpui::Quad>,
    pub shadows: Vec<gpui::Shadow>,
    pub underlines: Vec<gpui::Underline>,
    pub monochrome_sprites: Vec<gpui::MonochromeSprite>,
    pub subpixel_sprites: Vec<gpui::SubpixelSprite>,
    pub polychrome_sprites: Vec<gpui::PolychromeSprite>,
    pub paths: Vec<SnapshotPath>,
    pub surfaces: Vec<gpui::PaintSurface>,
    /// The paired surface opacities (consumed by the surface draw path
    /// once imported surface sources exist; the pinned import error for
    /// the ABI's Unsupported stand-in fires first today).
    #[expect(dead_code)]
    pub surface_opacities: Vec<f32>,
    pub backdrops: Vec<gpui::BackdropFilter>,
    pub boundaries: Vec<gpui::FilterBoundary>,
    pub commands: Vec<GpuiGoSceneCommandRecord>,
    pub requirements: GpuiGoSceneRequirementsRecord,
}

fn snapshot_bounds(bounds: &GpuiGoSceneBounds) -> gpui::Bounds<gpui::ScaledPixels> {
    gpui::Bounds {
        origin: point(gpui::ScaledPixels(f32::from_bits(bounds.x)), gpui::ScaledPixels(f32::from_bits(bounds.y))),
        size: gpui::Size {
            width: gpui::ScaledPixels(f32::from_bits(bounds.w)),
            height: gpui::ScaledPixels(f32::from_bits(bounds.h)),
        },
    }
}

fn snapshot_mask(mask: &GpuiGoSceneBounds) -> gpui::ContentMask<gpui::ScaledPixels> {
    gpui::ContentMask {
        bounds: snapshot_bounds(mask),
        ..Default::default()
    }
}

fn snapshot_radii(bits: &[u32]) -> gpui::Corners<gpui::ScaledPixels> {
    gpui::Corners {
        top_left: gpui::ScaledPixels(f32::from_bits(bits[0])),
        top_right: gpui::ScaledPixels(f32::from_bits(bits[1])),
        bottom_right: gpui::ScaledPixels(f32::from_bits(bits[2])),
        bottom_left: gpui::ScaledPixels(f32::from_bits(bits[3])),
    }
}

fn snapshot_edges(bits: &[u32]) -> gpui::Edges<gpui::ScaledPixels> {
    gpui::Edges {
        top: gpui::ScaledPixels(f32::from_bits(bits[0])),
        right: gpui::ScaledPixels(f32::from_bits(bits[1])),
        bottom: gpui::ScaledPixels(f32::from_bits(bits[2])),
        left: gpui::ScaledPixels(f32::from_bits(bits[3])),
    }
}

fn snapshot_color(color: &GpuiGoSceneColor) -> gpui::Background {
    // The ABI carries the stored solid HSLA record; the public
    // conversion is `Background::from(Hsla)` (the pin's paint path).
    let hsla: gpui::Hsla = gpui::Hsla::new(
        f32::from_bits(color.h) * 360.0,
        f32::from_bits(color.s),
        f32::from_bits(color.l),
        f32::from_bits(color.a),
    );
    gpui::Background::from(hsla)
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

fn snapshot_filters(rec: &GpuiGoSceneFilterRecord) -> impl Iterator<Item = gpui::ScaledFilter> + '_ {
    (0..rec.filter_count as usize).map(|index| {
        gpui::ScaledFilter::Blur(gpui::ScaledPixels(f32::from_bits(rec.blur_radii[index])))
    })
}

fn snapshot_quad(rec: &GpuiGoSceneQuadRecord) -> gpui::Quad {
    gpui::Quad {
        order: rec.order,
        border_style: if rec.border_style == 0 {
            gpui::BorderStyle::Solid
        } else {
            gpui::BorderStyle::Dashed
        },
        border_dashed_length: f32::from_bits(rec.border_dashed_length),
        border_dashed_gap: f32::from_bits(rec.border_dashed_gap),
        bounds: snapshot_bounds(&rec.bounds),
        content_mask: snapshot_mask(&rec.content_mask),
        background: snapshot_color(&rec.background),
        border_color: snapshot_color(&rec.border_color),
        corner_radii: snapshot_radii(&rec.corner_radii),
        border_widths: snapshot_edges(&rec.border_widths),
        corner_smoothing: f32::from_bits(rec.corner_smoothing),
        padding: rec.padding,
    }
}

fn snapshot_shadow(rec: &GpuiGoSceneShadowRecord) -> gpui::Shadow {
    gpui::Shadow {
        order: rec.order,
        blur_radius: gpui::ScaledPixels(f32::from_bits(rec.blur_radius)),
        bounds: snapshot_bounds(&rec.bounds),
        corner_radii: snapshot_radii(&rec.corner_radii),
        content_mask: snapshot_mask(&rec.content_mask),
        color: snapshot_color(&rec.color),
        element_bounds: snapshot_bounds(&rec.element_bounds),
        element_corner_radii: snapshot_radii(&rec.element_corner_radii),
        inset: gpui::ShaderBool::from(rec.inset != 0),
        corner_smoothing: f32::from_bits(rec.corner_smoothing),
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
    let clipped = SceneState::clipped_bounds(&data.rec.bounds, &data.rec.content_mask);
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

fn snapshot_surface(rec: &GpuiGoSceneSurfaceRecord) -> gpui::PaintSurface {
    // The ABI surface source is the pinned `SurfaceSource::Unsupported`
    // stand-in (tag 0); the pinned renderer's draw path bails on it
    // explicitly ("DirectX renderer cannot import this surface source"),
    // which the renderer service preserves as
    // `renderer_status::ERR_UNSUPPORTED_SOURCE`.
    gpui::PaintSurface {
        order: rec.order,
        bounds: snapshot_bounds(&rec.bounds),
        content_mask: snapshot_mask(&rec.content_mask),
        source: gpui::SurfaceSource::Unsupported(gpui::Size::default()),
    }
}

fn snapshot_backdrop(rec: &GpuiGoSceneFilterRecord) -> gpui::BackdropFilter {
    gpui::BackdropFilter {
        order: rec.order,
        bounds: snapshot_bounds(&rec.bounds),
        corner_radii: snapshot_radii(&rec.corner_radii),
        content_mask: snapshot_mask(&rec.content_mask),
        corner_smoothing: f32::from_bits(rec.corner_smoothing),
        filters: snapshot_filters(rec).collect(),
        opacity: f32::from_bits(rec.opacity),
    }
}

fn snapshot_boundary(rec: &GpuiGoSceneFilterRecord) -> gpui::FilterBoundary {
    gpui::FilterBoundary {
        order: rec.order,
        bounds: snapshot_bounds(&rec.bounds),
        content_mask: snapshot_mask(&rec.content_mask),
        corner_radii: snapshot_radii(&rec.corner_radii),
        corner_smoothing: f32::from_bits(rec.corner_smoothing),
        filters: snapshot_filters(rec).collect(),
        opacity: f32::from_bits(rec.opacity),
        is_start: rec.is_start != 0,
    }
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
    let snapshot = SceneRenderSnapshot {
        quads: state.quads.iter().map(snapshot_quad).collect(),
        shadows: state.shadows.iter().map(snapshot_shadow).collect(),
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
        backdrops: state.backdrops.iter().map(snapshot_backdrop).collect(),
        boundaries: state.boundaries.iter().map(snapshot_boundary).collect(),
        commands: state.plan.commands.clone(),
        requirements: state.plan.requirements,
    };
    Ok(snapshot)
}


/// The scene service table installed in reserved slot 3 of
/// `GpuiGoAbiTable`. All 26 function pointers come first (208 bytes),
/// then 17 self-check scalars (68 bytes). Offsets (x86-64): pointers
/// @0..208, `service_version` @208, `size_of_table` @212,
/// `align_of_table` @216, `size_of_quad_record` @220,
/// `size_of_shadow_record` @224, `size_of_underline_record` @228,
/// `size_of_filter_record` @232, `size_of_surface_record` @236,
/// `size_of_monochrome_sprite_record` @240,
/// `size_of_polychrome_sprite_record` @244,
/// `size_of_command_record` @248, `size_of_requirements_record` @252,
/// `size_of_meta_record` @256, `size_of_path_record` @260,
/// `size_of_path_vertex_record` @264, `size_of_path_command_record`
/// @268, `max_filters` @272; total size 280 (276 rounded up to the
/// table's 8-byte alignment), alignment 8.
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

    fn mask() -> GpuiGoSceneBounds {
        full_bounds()
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
            bounds,
            content_mask: bounds,
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
            blur_radii: [8.0f32.to_bits(), 0, 0, 0],
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
            blur_radii: [20.0f32.to_bits(), 0, 0, 0],
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
        // The pinned three-nested-group test: targets 0, 1, then inline;
        // each end carries its matched start's index.
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
                "begin:Inline",
                "quad 2..3",
                "end:Inline[2->3]",
                "end:Isolated(1)[1->4]",
                "end:Isolated(0)[0->5]",
            ]
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
        // order: Shadow(1) < Quad(2) < Underline(4) < Surface(8) <
        // BackdropFilter(9). The boundaries take orders above all prior
        // content (2 and 3) and bracket nothing in this degenerate shape.
        let wide_mask = GpuiGoSceneBounds::from_f32(F32Bounds {
            origin_x: 0.0,
            origin_y: 0.0,
            width: 1000.0,
            height: 1000.0,
        });
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
            blur_radii: [4.0f32.to_bits(), 0, 0, 0],
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
            color: GpuiGoSceneColor {
                tag: 0,
                h: 0.5f32.to_bits(),
                s: 0.5f32.to_bits(),
                l: 0.5f32.to_bits(),
                a: 1.0f32.to_bits(),
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
