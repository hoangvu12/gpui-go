//! gpui-go native texture atlas service (ticket10, reserved slot 6).
//!
//! A port of the pinned Windows sprite atlas,
//! `crates/gpui_windows/src/directx_atlas.rs` at
//! `254b5dbd47cbb5acbcc5bbdcbb322a339276c88a` (all citations below are
//! `directx_atlas.rs:<lines>` at that commit), exposed through a private
//! C ABI service table.
//!
//! # The design decisions the ticket asked to record
//!
//! * **Allocation algorithm** — the pin's `etagere::BucketedAtlasAllocator`
//!   (directx_atlas.rs:31, the `DirectXAtlasTexture::allocator` field; the
//!   workspace lock pins etagere 0.2.15, the version this artifact's
//!   Cargo.toml pins exactly). Tile ids are the serialized `etagere::AllocId`
//!   values, exactly as the pin's `TileId` (crates/gpui/src/platform.rs:
//!   `impl From<AllocId> for TileId { Self(id.serialize()) }`).
//! * **Texture pools** — three separate pools exactly as the pin
//!   (directx_atlas.rs:23-27): monochrome (`DXGI_FORMAT_R8_UNORM`, 1 byte
//!   per pixel), polychrome and subpixel (`DXGI_FORMAT_B8G8R8A8_UNORM`, 4
//!   bytes per pixel with different pixel meanings). New textures are
//!   1024x1024 by default, clamped to the D3D11 16384 maximum
//!   (`push_texture`, directx_atlas.rs:363-373); freed texture slots go
//!   on the free list and are reused (directx_atlas.rs:414-420).
//! * **Upload validation** — the pin validates the exact byte count
//!   before allocation or publication (directx_atlas.rs:78-85, calling
//!   `AtlasTextureKind::validate_upload`), rejects dimensions above the
//!   16384 texture limit, and uploads through
//!   `ID3D11DeviceContext::UpdateSubresource` with the row pitch
//!   `width * bytes_per_pixel` (directx_atlas.rs:435-456).
//! * **Device sharing** — the atlas is created from the RENDERER
//!   service's shared per-process device and immediate context
//!   (`crate::renderer`'s `DeviceShared`, created exactly like the pin's
//!   `DirectXDevices::new`), mirroring the pin where `DirectXAtlas::new`
//!   receives the renderer's device and context
//!   (crates/gpui_windows/src/directx_renderer.rs:349:
//!   `DirectXAtlas::new(&devices.device, &devices.device_context)`).
//!   The context is the multithread-protected immediate context the
//!   renderer service created (`SetMultithreadProtected(true)`), and the
//!   atlas serializes every entry behind its own state mutex — no lock is
//!   held across callbacks (the service has none).
//! * **Tile removal** — the pin's `remove` (directx_atlas.rs:91-122):
//!   deallocate the etagere slot, decrement the texture's live-key count,
//!   and return the texture itself to the free list when it becomes
//!   unreferenced.
//! * **Device loss** — the pin's `handle_device_lost`
//!   (directx_atlas.rs:63-76): clear all three texture pools and the tile
//!   map. The ABI adds a generation counter: `notify_device_lost` clears
//!   every physical tile and bumps the generation, so previously returned
//!   tile records become observably stale (queries miss) — the renderer
//!   contract's "device generation changes invalidate all physical tiles,
//!   even if a logical font/image resource remains alive". The entry also
//!   re-binds to the renderer service's current device (which recreates
//!   it if the renderer service has already recovered), exactly like the
//!   pin's rebind-to-new-device parameters.
//!
//! # Deviations from the pin (each mechanical, none silent)
//!
//! * The pin's `get_or_insert_with(key, &mut build)` laziness cannot
//!   cross an ABI (the renderer contract requires exactly that: "Go
//!   obtains raster bytes before calling the atlas insertion seam").
//!   The ABI splits the operation: `atlas_insert(key, size, bytes)`
//!   performs the same map check first (a cached hit returns the existing
//!   tile with NO upload and NO new allocation), and the caller (Go)
//!   rasterizes before inserting. The renderer contract's
//!   "Native atlas code may not call an arbitrary Go builder while
//!   holding its mutex" is satisfied by construction.
//! * `atlas_query`/`atlas_texture_count`/`atlas_tile_count`/
//!   `atlas_generation` are ABI observability entries; the pin exposes
//!   `contains` only under test-support. They read state; they do not
//!   alter the pinned behavior.
//! * The pin's `get_texture_view` (renderer-side SRV lookup) is not
//!   exposed yet: the sprite draw pipeline is a later ticket; the ABI
//!   gains it when the renderer draws sprites.
//! * The mutex is a `std::sync::Mutex` (the service's own), not the
//!   pin's `parking_lot::Mutex`: the semantics are identical here (no
//!   reentrancy, no callbacks under the lock).
//!
//! # ABI rules (same conventions as RENDERER_ABI.md)
//!
//! * C ABI, `extern "system"`, `#[repr(C)]`, fixed-width records with
//!   self-checks; every export contains panics with `catch_unwind`.
//! * Atlas handles encode `(slot index << 20) | slot generation`
//!   (the scene/layout encoding). Stale after dispose or slot reuse;
//!   malformed handles are typed errors.
//! * The byte buffer of `atlas_insert` is borrowed for the call only and
//!   copied into the upload path synchronously; no Go pointer is
//!   retained.

use std::collections::HashMap;
use std::panic::{catch_unwind, AssertUnwindSafe};
use std::sync::Mutex;

use etagere::BucketedAtlasAllocator;
use windows::Win32::Graphics::{
    Direct3D11::{
        D3D11_BIND_SHADER_RESOURCE, D3D11_BOX, D3D11_TEXTURE2D_DESC, D3D11_USAGE_DEFAULT,
        ID3D11Device, ID3D11DeviceContext, ID3D11ShaderResourceView, ID3D11Texture2D,
    },
    Dxgi::Common::*,
};

use crate::glyph::{GpuiGoRasterStyleRecord, raster_effect, raster_format, raster_mode};

/// Atlas service ABI version.
pub const GPUI_GO_ATLAS_SERVICE_VERSION: u32 = 1;

/// Live atlas instances (handle slots).
pub const MAX_ATLAS_SLOTS: u32 = 8;

/// The pin's texture dimension bounds (directx_atlas.rs:364-372).
pub const DEFAULT_ATLAS_SIZE: i32 = 1024;
pub const MAX_ATLAS_SIZE: i32 = 16384;

/// Atlas service status codes (mirrored in internal/native/atlas.go).
pub mod atlas_status {
    /// Success.
    pub const OK: i32 = 0;
    /// The atlas handle encoded a past generation (disposed or slot
    /// reused).
    pub const ERR_STALE_HANDLE: i32 = -1;
    /// The handle never existed or is malformed.
    pub const ERR_BAD_HANDLE: i32 = -2;
    /// Null argument.
    pub const ERR_NULL_ARG: i32 = -3;
    /// Bad value (key tag/record, non-positive or oversized dimensions,
    /// byte-count mismatch against the texture kind, reserved fields).
    pub const ERR_BAD_VALUE: i32 = -4;
    /// Device failure (texture or view creation failed — the pin's
    /// `push_texture` returning `None`).
    pub const ERR_DEVICE: i32 = -5;
    /// A query missed: the key has no live tile.
    pub const ERR_NOT_FOUND: i32 = -8;
    /// A panic was contained by catch_unwind before returning.
    pub const ERR_PANIC: i32 = 101;
}

/// Texture kind tags (the pinned `AtlasTextureKind` discriminants,
/// crates/gpui/src/platform.rs:1950).
pub mod atlas_texture_kind {
    /// One coverage byte per pixel, `DXGI_FORMAT_R8_UNORM`.
    pub const MONOCHROME: u32 = 0;
    /// Four BGRA bytes per pixel (color), `DXGI_FORMAT_B8G8R8A8_UNORM`.
    pub const POLYCHROME: u32 = 1;
    /// Four BGRA bytes per pixel (LCD coverage), `DXGI_FORMAT_B8G8R8A8_UNORM`.
    pub const SUBPIXEL: u32 = 2;
}

/// Atlas key kind tags (the pinned `AtlasKey` variants,
/// crates/gpui/src/platform.rs:1815). Svg keys remain a later ticket
/// and are rejected as bad values here.
pub mod atlas_key_kind {
    /// A glyph key: `(RenderGlyphParams, RasterizedGlyphFormat)`.
    pub const GLYPH: u32 = 0;
    /// An image key: `RenderImageParams { image_id, frame_index }`
    /// (ticket17; the pinned `AtlasKey::Image`, texture kind
    /// polychrome). The record reuses the glyph slots: `image_id`
    /// occupies the `font_id` slot, `frame_index` the
    /// `font_size_bits` slot, and every other field must be zero.
    pub const IMAGE: u32 = 1;
}

// ---------------------------------------------------------------------------
// ABI records
// ---------------------------------------------------------------------------

/// Atlas key record (the glyph variant of the pinned `AtlasKey`:
/// `RenderGlyphParams` plus the `RasterizedGlyphFormat`). Layout
/// (x86-64, `#[repr(C)]`): `kind` @0, 4 bytes padding, `font_id` @8,
/// `glyph_id` @16, `font_size_bits` @20, `subpixel_x` @24,
/// `subpixel_y` @28, `scale_bits` @32, `style` @36 (the 36-byte
/// raster-style record), `format` @72, `reserved[2]` @76, `record_size`
/// @84; size 88, alignment 8.
#[repr(C)]
pub struct GpuiGoAtlasKeyRecord {
    /// Key kind tag; see [`atlas_key_kind`].
    pub kind: u32,
    /// The canonical font id of the glyph's exact face/instance.
    pub font_id: u64,
    /// The glyph id.
    pub glyph_id: u32,
    /// The font size in pixels (f32 bits).
    pub font_size_bits: u32,
    /// The subpixel variant x (0..SUBPIXEL_VARIANTS_X).
    pub subpixel_x: u32,
    /// The subpixel variant y (0..SUBPIXEL_VARIANTS_Y).
    pub subpixel_y: u32,
    /// The scale factor (f32 bits).
    pub scale_bits: u32,
    /// The prepared raster style (the cache-relevant settings).
    pub style: GpuiGoRasterStyleRecord,
    /// The rasterized format tag; see [`raster_format`].
    pub format: u32,
    /// Reserved; must be 0.
    pub reserved: [u32; 2],
    /// Self-check: `size_of::<GpuiGoAtlasKeyRecord>()`.
    pub record_size: u32,
}

impl Default for GpuiGoAtlasKeyRecord {
    fn default() -> Self {
        Self {
            kind: atlas_key_kind::GLYPH,
            font_id: 0,
            glyph_id: 0,
            font_size_bits: 0,
            subpixel_x: 0,
            subpixel_y: 0,
            scale_bits: 0,
            style: GpuiGoRasterStyleRecord::default(),
            format: raster_format::ALPHA_MASK,
            reserved: [0; 2],
            record_size: core::mem::size_of::<GpuiGoAtlasKeyRecord>() as u32,
        }
    }
}

/// Atlas tile record (the pinned `AtlasTile`:
/// `crates/gpui/src/platform.rs:1918`). Layout: `texture_index` @0,
/// `texture_kind` @4, `tile_id` @8, `padding` @12, `bounds_x` @16,
/// `bounds_y` @20, `bounds_w` @24, `bounds_h` @28, `generation` @32
/// (the atlas generation that produced this tile), `record_size` @36;
/// size 40, alignment 4.
#[repr(C)]
#[derive(Clone, Copy)]
pub struct GpuiGoAtlasTileRecord {
    /// The index of this tile's texture within its kind pool.
    pub texture_index: u32,
    /// The texture kind tag; see [`atlas_texture_kind`].
    pub texture_kind: u32,
    /// The unique id of this tile within its texture (the serialized
    /// etagere AllocId).
    pub tile_id: u32,
    /// Padding around the tile content in pixels (0 for glyph tiles).
    pub padding: u32,
    /// The tile bounds origin x within the texture.
    pub bounds_x: i32,
    /// The tile bounds origin y within the texture.
    pub bounds_y: i32,
    /// The tile bounds width within the texture.
    pub bounds_w: i32,
    /// The tile bounds height within the texture.
    pub bounds_h: i32,
    /// The atlas generation at insertion (tiles from an earlier
    /// generation are invalid after device loss).
    pub generation: u32,
    /// Self-check: `size_of::<GpuiGoAtlasTileRecord>()`.
    pub record_size: u32,
}

impl Default for GpuiGoAtlasTileRecord {
    fn default() -> Self {
        Self {
            texture_index: 0,
            texture_kind: atlas_texture_kind::MONOCHROME,
            tile_id: 0,
            padding: 0,
            bounds_x: 0,
            bounds_y: 0,
            bounds_w: 0,
            bounds_h: 0,
            generation: 0,
            record_size: core::mem::size_of::<GpuiGoAtlasTileRecord>() as u32,
        }
    }
}

const _: () = assert!(std::mem::size_of::<GpuiGoAtlasKeyRecord>() == 88);
const _: () = assert!(std::mem::align_of::<GpuiGoAtlasKeyRecord>() == 8);
const _: () = assert!(std::mem::size_of::<GpuiGoAtlasTileRecord>() == 40);
const _: () = assert!(std::mem::align_of::<GpuiGoAtlasTileRecord>() == 4);

// ---------------------------------------------------------------------------
// Service table
// ---------------------------------------------------------------------------

/// The atlas service table: 10 function pointers then 9 self-check
/// scalars; size 120 (116 bytes of fields rounded to the 8-byte
/// alignment), alignment 8. All entries return an i32 status.
#[repr(C)]
pub struct GpuiGoAtlasTable {
    /// `atlas_create`.
    pub create: Option<unsafe extern "system" fn(out_handle: *mut u64) -> i32>,
    /// `atlas_dispose`.
    pub dispose: Option<unsafe extern "system" fn(handle: u64) -> i32>,
    /// `atlas_insert` (validated upload; map hit returns the cached tile).
    pub insert: Option<
        unsafe extern "system" fn(
            handle: u64,
            key: *const GpuiGoAtlasKeyRecord,
            width: i32,
            height: i32,
            bytes: *const u8,
            byte_count: u32,
            out_tile: *mut GpuiGoAtlasTileRecord,
        ) -> i32,
    >,
    /// `atlas_remove`.
    pub remove: Option<unsafe extern "system" fn(handle: u64, key: *const GpuiGoAtlasKeyRecord) -> i32>,
    /// `atlas_query`.
    pub query: Option<
        unsafe extern "system" fn(
            handle: u64,
            key: *const GpuiGoAtlasKeyRecord,
            out_tile: *mut GpuiGoAtlasTileRecord,
        ) -> i32,
    >,
    /// `atlas_texture_count` (per kind pool).
    pub texture_count: Option<unsafe extern "system" fn(handle: u64, kind: u32, out_count: *mut u32) -> i32>,
    /// `atlas_tile_count` (live keys).
    pub tile_count: Option<unsafe extern "system" fn(handle: u64, out_count: *mut u32) -> i32>,
    /// `atlas_generation`.
    pub generation: Option<unsafe extern "system" fn(handle: u64, out_generation: *mut u32) -> i32>,
    /// `atlas_notify_device_lost`.
    pub notify_device_lost: Option<unsafe extern "system" fn(handle: u64) -> i32>,
    /// `atlas_panic_probe` (test-only; panics inside catch_unwind).
    pub panic_probe: Option<unsafe extern "system" fn() -> i32>,
    /// Atlas ABI version.
    pub service_version: u32,
    /// `size_of::<GpuiGoAtlasTable>()` self-check.
    pub size_of_table: u32,
    /// `align_of::<GpuiGoAtlasTable>()` self-check.
    pub align_of_table: u32,
    /// `size_of::<GpuiGoAtlasKeyRecord>()` self-check.
    pub size_of_key_record: u32,
    /// `align_of::<GpuiGoAtlasKeyRecord>()` self-check.
    pub align_of_key_record: u32,
    /// `size_of::<GpuiGoAtlasTileRecord>()` self-check.
    pub size_of_tile_record: u32,
    /// [`DEFAULT_ATLAS_SIZE`].
    pub default_atlas_size: u32,
    /// [`MAX_ATLAS_SIZE`].
    pub max_atlas_size: u32,
    /// [`MAX_ATLAS_SLOTS`].
    pub max_atlas_slots: u32,
}

const _: () = assert!(std::mem::size_of::<GpuiGoAtlasTable>() == 120);
const _: () = assert!(std::mem::align_of::<GpuiGoAtlasTable>() == 8);

/// The one static atlas service table.
pub(crate) static ATLAS_TABLE: GpuiGoAtlasTable = GpuiGoAtlasTable {
    create: Some(atlas_create),
    dispose: Some(atlas_dispose),
    insert: Some(atlas_insert),
    remove: Some(atlas_remove),
    query: Some(atlas_query),
    texture_count: Some(atlas_texture_count),
    tile_count: Some(atlas_tile_count),
    generation: Some(atlas_generation),
    notify_device_lost: Some(atlas_notify_device_lost),
    panic_probe: Some(atlas_panic_probe),
    service_version: GPUI_GO_ATLAS_SERVICE_VERSION,
    size_of_table: std::mem::size_of::<GpuiGoAtlasTable>() as u32,
    align_of_table: std::mem::align_of::<GpuiGoAtlasTable>() as u32,
    size_of_key_record: std::mem::size_of::<GpuiGoAtlasKeyRecord>() as u32,
    align_of_key_record: std::mem::align_of::<GpuiGoAtlasKeyRecord>() as u32,
    size_of_tile_record: std::mem::size_of::<GpuiGoAtlasTileRecord>() as u32,
    default_atlas_size: DEFAULT_ATLAS_SIZE as u32,
    max_atlas_size: MAX_ATLAS_SIZE as u32,
    max_atlas_slots: MAX_ATLAS_SLOTS,
};

// ---------------------------------------------------------------------------
// Internal state
// ---------------------------------------------------------------------------

/// The semantic atlas key (the pin's `AtlasKey` enum: the glyph
/// variant's `(RenderGlyphParams, RasterizedGlyphFormat)` content, or
/// the image variant's `RenderImageParams { image_id, frame_index }`).
#[derive(Clone, Copy, PartialEq, Eq, Hash)]
enum AtlasKey {
    /// A glyph raster's cache identity.
    Glyph {
        font_id: u64,
        glyph_id: u32,
        font_size_bits: u32,
        subpixel_x: u32,
        subpixel_y: u32,
        scale_bits: u32,
        style_mode: u32,
        style_effect: u32,
        style_color: [u32; 4],
        format: u32,
    },
    /// A decoded image frame's cache identity (ticket17; the pinned
    /// `AtlasKey::Image`).
    Image {
        image_id: u64,
        frame_index: u32,
    },
}

impl AtlasKey {
    fn texture_kind(&self) -> u32 {
        match self {
            AtlasKey::Glyph { format, .. } => match *format {
                raster_format::ALPHA_MASK => atlas_texture_kind::MONOCHROME,
                raster_format::BGRA_SUBPIXEL_MASK => atlas_texture_kind::SUBPIXEL,
                raster_format::BGRA_COLOR => atlas_texture_kind::POLYCHROME,
                _ => atlas_texture_kind::MONOCHROME,
            },
            // The pinned `AtlasKey::Image` mapping: polychrome.
            AtlasKey::Image { .. } => atlas_texture_kind::POLYCHROME,
        }
    }
}

/// One atlas texture (the pin's `DirectXAtlasTexture`: the D3D11 texture
/// plus its shader-resource view — created eagerly by the pin at texture
/// creation, lazily by this service at the first `texture_srv` lookup,
/// the same object lifecycle either way: the view pins the texture).
struct AtlasTexture {
    index: u32,
    kind: u32,
    bytes_per_pixel: u32,
    allocator: BucketedAtlasAllocator,
    texture: ID3D11Texture2D,
    /// The texture's shader-resource view (the pin's `view` field).
    view: Option<ID3D11ShaderResourceView>,
    live_atlas_keys: u32,
}

/// The pin's `AtlasTextureList` (crates/gpui/src/platform.rs:1880): a
/// sparse texture array plus its free list.
#[derive(Default)]
struct AtlasTextureList {
    textures: Vec<Option<AtlasTexture>>,
    free_list: Vec<usize>,
}

impl AtlasTextureList {
    fn iter_mut(&mut self) -> impl DoubleEndedIterator<Item = &mut AtlasTexture> {
        self.textures.iter_mut().flatten()
    }
}

/// One atlas instance (the pin's `DirectXAtlasState` plus the ABI
/// generation).
struct AtlasState {
    device: ID3D11Device,
    device_context: ID3D11DeviceContext,
    monochrome_textures: AtlasTextureList,
    polychrome_textures: AtlasTextureList,
    subpixel_textures: AtlasTextureList,
    tiles_by_key: HashMap<AtlasKey, GpuiGoAtlasTileRecord>,
    /// Bumped by device-loss invalidation; carried by tile records.
    generation: u32,
}

struct AtlasSlot {
    /// Slot generation of the last atlas that occupied this slot
    /// (persists while vacant so disposed handles stay stale after slot
    /// reuse).
    slot_generation: u32,
    occupied: bool,
    atlas: Option<AtlasState>,
}

impl AtlasSlot {
    fn vacant() -> Self {
        AtlasSlot { slot_generation: 0, occupied: false, atlas: None }
    }
}

struct AtlasServiceState {
    slots: Vec<AtlasSlot>,
}

static GLOBAL: Mutex<AtlasServiceState> =
    Mutex::new(AtlasServiceState { slots: Vec::new() });

// SAFETY: the atlas state contains raw COM interface wrappers, which the
// windows crate conservatively treats as !Send. The device/context are
// the renderer service's multithread-protected objects and every access
// is serialized behind the GLOBAL mutex, so no COM object is used
// concurrently.
unsafe impl Send for AtlasServiceState {}

fn lock_global() -> std::sync::MutexGuard<'static, AtlasServiceState> {
    GLOBAL.lock().unwrap_or_else(|poisoned| poisoned.into_inner())
}

const ATLAS_HANDLE_SLOT_SHIFT: u32 = 20;
const ATLAS_GENERATION_MAX: u32 = 0x000F_FFFF;

fn atlas_handle_parts(handle: u64) -> Result<(usize, u32), i32> {
    let slot = (handle >> ATLAS_HANDLE_SLOT_SHIFT) as usize;
    let generation = (handle & ATLAS_GENERATION_MAX as u64) as u32;
    if slot >= MAX_ATLAS_SLOTS as usize {
        return Err(atlas_status::ERR_BAD_HANDLE);
    }
    if generation == 0 {
        return Err(atlas_status::ERR_BAD_HANDLE);
    }
    Ok((slot, generation))
}

fn make_atlas_handle(slot: usize, generation: u32) -> u64 {
    ((slot as u64) << ATLAS_HANDLE_SLOT_SHIFT) | generation as u64
}

/// The pin's upload validation (`AtlasTextureKind::validate_upload`,
/// crates/gpui/src/platform.rs:1972-1995): positive dimensions and the
/// exact channel count for the texture kind.
fn validate_upload(kind: u32, width: i32, height: i32, byte_count: u32) -> Result<(), i32> {
    if width <= 0 || height <= 0 {
        return Err(atlas_status::ERR_BAD_VALUE);
    }
    let channels: u32 = match kind {
        atlas_texture_kind::MONOCHROME => 1,
        atlas_texture_kind::POLYCHROME | atlas_texture_kind::SUBPIXEL => 4,
        _ => return Err(atlas_status::ERR_BAD_VALUE),
    };
    let width = u32::try_from(width).map_err(|_| atlas_status::ERR_BAD_VALUE)?;
    let height = u32::try_from(height).map_err(|_| atlas_status::ERR_BAD_VALUE)?;
    let expected = width
        .checked_mul(height)
        .and_then(|pixels| pixels.checked_mul(channels))
        .ok_or(atlas_status::ERR_BAD_VALUE)?;
    if byte_count != expected {
        return Err(atlas_status::ERR_BAD_VALUE);
    }
    Ok(())
}

/// Decodes and validates a key record into the semantic key.
fn key_from_record(record: &GpuiGoAtlasKeyRecord) -> Result<AtlasKey, i32> {
    if record.record_size != core::mem::size_of::<GpuiGoAtlasKeyRecord>() as u32 {
        return Err(atlas_status::ERR_BAD_VALUE);
    }
    if record.kind == atlas_key_kind::IMAGE {
        // The image key (ticket17): image_id reuses the font_id slot,
        // frame_index the font_size_bits slot; every other field must
        // be zero (a clean image key, never a half-filled glyph key).
        if record.reserved != [0, 0]
            || record.glyph_id != 0
            || record.subpixel_x != 0
            || record.subpixel_y != 0
            || record.scale_bits != 0
            || record.style.record_size != 0
            || record.style.dilation != 0
            || record.style.reserved != 0
            || record.style.mode != 0
            || record.style.color_effect_tag != 0
            || record.style.color_r != 0
            || record.style.color_g != 0
            || record.style.color_b != 0
            || record.style.color_a != 0
            || record.format != 0
        {
            return Err(atlas_status::ERR_BAD_VALUE);
        }
        return Ok(AtlasKey::Image {
            image_id: record.font_id,
            frame_index: record.font_size_bits,
        });
    }
    if record.kind != atlas_key_kind::GLYPH {
        // Svg keys remain a later ticket; not silently accepted.
        return Err(atlas_status::ERR_BAD_VALUE);
    }
    if record.reserved != [0, 0] {
        return Err(atlas_status::ERR_BAD_VALUE);
    }
    if record.style.record_size != core::mem::size_of::<GpuiGoRasterStyleRecord>() as u32 {
        return Err(atlas_status::ERR_BAD_VALUE);
    }
    if record.style.dilation != 0 || record.style.reserved != 0 {
        return Err(atlas_status::ERR_BAD_VALUE);
    }
    if !matches!(
        record.style.mode,
        raster_mode::GRAYSCALE | raster_mode::SUBPIXEL | raster_mode::COLOR
    ) {
        return Err(atlas_status::ERR_BAD_VALUE);
    }
    if !matches!(
        record.style.color_effect_tag,
        raster_effect::INDEPENDENT | raster_effect::PREBLEND
    ) {
        return Err(atlas_status::ERR_BAD_VALUE);
    }
    if !matches!(
        record.format,
        raster_format::ALPHA_MASK | raster_format::BGRA_SUBPIXEL_MASK | raster_format::BGRA_COLOR
    ) {
        return Err(atlas_status::ERR_BAD_VALUE);
    }
    let font_size = f32::from_bits(record.font_size_bits);
    let scale = f32::from_bits(record.scale_bits);
    if !font_size.is_finite() || font_size < 0.0 {
        return Err(atlas_status::ERR_BAD_VALUE);
    }
    if !scale.is_finite() || scale <= 0.0 {
        return Err(atlas_status::ERR_BAD_VALUE);
    }
    Ok(AtlasKey::Glyph {
        font_id: record.font_id,
        glyph_id: record.glyph_id,
        font_size_bits: record.font_size_bits,
        subpixel_x: record.subpixel_x,
        subpixel_y: record.subpixel_y,
        scale_bits: record.scale_bits,
        style_mode: record.style.mode,
        style_effect: record.style.color_effect_tag,
        style_color: [
            record.style.color_r,
            record.style.color_g,
            record.style.color_b,
            record.style.color_a,
        ],
        format: record.format,
    })
}

impl AtlasState {
    /// The pin's `allocate` (directx_atlas.rs:124-140): last-fit over the
    /// existing textures of the kind, then a new texture.
    fn allocate(
        &mut self,
        width: i32,
        height: i32,
        kind: u32,
    ) -> Result<Option<(u32, u32, u32, i32, i32, i32)>, i32> {
        {
            let textures = match kind {
                atlas_texture_kind::MONOCHROME => &mut self.monochrome_textures,
                atlas_texture_kind::POLYCHROME => &mut self.polychrome_textures,
                atlas_texture_kind::SUBPIXEL => &mut self.subpixel_textures,
                _ => return Err(atlas_status::ERR_BAD_VALUE),
            };

            if let Some(tile) = textures
                .iter_mut()
                .rev()
                .find_map(|texture| Self::texture_allocate(texture, width, height))
            {
                return Ok(Some(tile));
            }
        }

        let texture = self.push_texture(width, height, kind)?;
        Ok(Self::texture_allocate(texture, width, height))
    }

    /// The pin's per-texture allocation (directx_atlas.rs:423-436).
    fn texture_allocate(
        texture: &mut AtlasTexture,
        width: i32,
        height: i32,
    ) -> Option<(u32, u32, u32, i32, i32, i32)> {
        let allocation = texture
            .allocator
            .allocate(etagere::Size::new(width, height))?;
        let tile = (
            texture.index,
            texture.kind,
            allocation.id.serialize(),
            allocation.rectangle.min.x,
            allocation.rectangle.min.y,
            width,
        );
        texture.live_atlas_keys += 1;
        Some(tile)
    }

    /// The pin's `push_texture` (directx_atlas.rs:142-221): default
    /// 1024x1024 clamped to 16384, the kind's pixel format, SRV bind
    /// flag, default usage, and the free-list slot reuse.
    fn push_texture(&mut self, min_width: i32, min_height: i32, kind: u32) -> Result<&mut AtlasTexture, i32> {
        let size = (
            min_width.min(MAX_ATLAS_SIZE).max(DEFAULT_ATLAS_SIZE),
            min_height.min(MAX_ATLAS_SIZE).max(DEFAULT_ATLAS_SIZE),
        );
        let (pixel_format, bind_flag, bytes_per_pixel) = match kind {
            atlas_texture_kind::MONOCHROME => (DXGI_FORMAT_R8_UNORM, D3D11_BIND_SHADER_RESOURCE, 1),
            atlas_texture_kind::POLYCHROME => {
                (DXGI_FORMAT_B8G8R8A8_UNORM, D3D11_BIND_SHADER_RESOURCE, 4)
            }
            atlas_texture_kind::SUBPIXEL => {
                (DXGI_FORMAT_B8G8R8A8_UNORM, D3D11_BIND_SHADER_RESOURCE, 4)
            }
            _ => return Err(atlas_status::ERR_BAD_VALUE),
        };
        let texture_desc = D3D11_TEXTURE2D_DESC {
            Width: size.0 as u32,
            Height: size.1 as u32,
            MipLevels: 1,
            ArraySize: 1,
            Format: pixel_format,
            SampleDesc: DXGI_SAMPLE_DESC { Count: 1, Quality: 0 },
            Usage: D3D11_USAGE_DEFAULT,
            BindFlags: bind_flag.0 as u32,
            CPUAccessFlags: 0,
            MiscFlags: 0,
        };
        let mut texture: Option<ID3D11Texture2D> = None;
        unsafe {
            // The pin: this only returns an error if the device is lost,
            // which the service recreates later (directx_atlas.rs:198-201).
            self.device
                .CreateTexture2D(&texture_desc, None, Some(&mut texture))
                .map_err(|_| atlas_status::ERR_DEVICE)?;
        }
        let texture = texture.expect("CreateTexture2D succeeded");

        let texture_list = match kind {
            atlas_texture_kind::MONOCHROME => &mut self.monochrome_textures,
            atlas_texture_kind::POLYCHROME => &mut self.polychrome_textures,
            atlas_texture_kind::SUBPIXEL => &mut self.subpixel_textures,
            _ => return Err(atlas_status::ERR_BAD_VALUE),
        };
        let index = texture_list.free_list.pop();
        let atlas_texture = AtlasTexture {
            index: index.unwrap_or(texture_list.textures.len()) as u32,
            kind,
            bytes_per_pixel,
            allocator: etagere::BucketedAtlasAllocator::new(etagere::Size::new(size.0, size.1)),
            texture,
            view: None,
            live_atlas_keys: 0,
        };
        if let Some(ix) = index {
            texture_list.textures[ix] = Some(atlas_texture);
            Ok(texture_list.textures.get_mut(ix).unwrap().as_mut().unwrap())
        } else {
            texture_list.textures.push(Some(atlas_texture));
            Ok(texture_list.textures.last_mut().unwrap().as_mut().unwrap())
        }
    }

    /// The pin's upload (directx_atlas.rs:438-456): UpdateSubresource
    /// with the exact tile box and the width row pitch.
    fn upload(&self, tile: &GpuiGoAtlasTileRecord, bytes: &[u8]) {
        let textures = match tile.texture_kind {
            atlas_texture_kind::MONOCHROME => &self.monochrome_textures,
            atlas_texture_kind::POLYCHROME => &self.polychrome_textures,
            atlas_texture_kind::SUBPIXEL => &self.subpixel_textures,
            _ => return,
        };
        let Some(texture) = textures
            .textures
            .get(tile.texture_index as usize)
            .and_then(|slot| slot.as_ref())
        else {
            return;
        };
        let left = tile.bounds_x as u32;
        let top = tile.bounds_y as u32;
        let right = left + tile.bounds_w as u32;
        let bottom = top + tile.bounds_h as u32;
        unsafe {
            self.device_context.UpdateSubresource(
                &texture.texture,
                0,
                Some(&D3D11_BOX {
                    left,
                    top,
                    front: 0,
                    right,
                    bottom,
                    back: 1,
                }),
                bytes.as_ptr() as _,
                (tile.bounds_w as u32) * texture.bytes_per_pixel,
                0,
            );
        }
    }

    /// The pin's `remove` (directx_atlas.rs:91-122): deallocate the
    /// etagere slot, decrement the live-key count, free-list the texture
    /// when unreferenced.
    fn remove(&mut self, key: &AtlasKey) {
        let Some(tile) = self.tiles_by_key.remove(key) else {
            return;
        };

        let textures = match tile.texture_kind {
            atlas_texture_kind::MONOCHROME => &mut self.monochrome_textures,
            atlas_texture_kind::POLYCHROME => &mut self.polychrome_textures,
            atlas_texture_kind::SUBPIXEL => &mut self.subpixel_textures,
            _ => return,
        };

        let Some(texture_slot) = textures.textures.get_mut(tile.texture_index as usize) else {
            return;
        };

        if let Some(mut texture) = texture_slot.take() {
            texture.allocator.deallocate(etagere::AllocId::deserialize(tile.tile_id));
            texture.live_atlas_keys -= 1;
            if texture.live_atlas_keys == 0 {
                textures.free_list.push(texture.index as usize);
            } else {
                *texture_slot = Some(texture);
            }
        }
    }
}

// ---------------------------------------------------------------------------
// Entry bodies
// ---------------------------------------------------------------------------

unsafe fn atlas_create_body(out_handle: *mut u64) -> i32 {
    unsafe {
        if out_handle.is_null() {
            return atlas_status::ERR_NULL_ARG;
        }
        *out_handle = 0;

        // The renderer service's shared device (the pinned construction
        // site: DirectXAtlas::new(&devices.device, &devices.device_context)).
        let device = match crate::renderer::shared_device() {
            Ok(device) => device,
            Err(code) => return code,
        };

        let mut state = lock_global();
        while state.slots.len() < MAX_ATLAS_SLOTS as usize {
            state.slots.push(AtlasSlot::vacant());
        }
        let slot_index = state.slots.iter().position(|slot| !slot.occupied);
        let Some(slot_index) = slot_index else {
            return atlas_status::ERR_BAD_HANDLE;
        };
        let slot = &mut state.slots[slot_index];
        let generation = match slot
            .slot_generation
            .checked_add(1)
            .filter(|generation| *generation <= ATLAS_GENERATION_MAX)
        {
            Some(generation) => generation,
            None => return atlas_status::ERR_BAD_HANDLE,
        };
        slot.slot_generation = generation;
        slot.occupied = true;
        slot.atlas = Some(AtlasState {
            device: device.device.clone(),
            device_context: device.context.clone(),
            monochrome_textures: AtlasTextureList::default(),
            polychrome_textures: AtlasTextureList::default(),
            subpixel_textures: AtlasTextureList::default(),
            tiles_by_key: HashMap::default(),
            generation: 1,
        });
        *out_handle = make_atlas_handle(slot_index, generation);
        atlas_status::OK
    }
}

/// Resolves a live atlas handle to its slot, mapping stale/malformed
/// handles to their status codes.
fn resolve_slot<'a>(
    state: &'a mut AtlasServiceState,
    handle: u64,
) -> Result<(usize, &'a mut AtlasState), i32> {
    let (slot_index, generation) = atlas_handle_parts(handle)?;
    let slot = state
        .slots
        .get_mut(slot_index)
        .ok_or(atlas_status::ERR_BAD_HANDLE)?;
    if !slot.occupied || slot.slot_generation != generation {
        return Err(atlas_status::ERR_STALE_HANDLE);
    }
    let atlas = slot.atlas.as_mut().ok_or(atlas_status::ERR_STALE_HANDLE)?;
    Ok((slot_index, atlas))
}

/// The renderer-facing texture-view lookup (the pin's
/// `DirectXAtlas::get_texture_view`, directx_atlas.rs:49-57): returns
/// the texture's shader-resource view of one pool (kind) at one index,
/// creating it lazily with the atlas's own device (the pin creates the
/// view at texture creation; both pin the texture for the view's
/// lifetime). Called by the renderer's draw path while it holds the
/// renderer lock — the atlas lock nests inside it, never the other way
/// around.
pub(crate) fn texture_srv(
    atlas: u64,
    kind: u32,
    index: u32,
) -> Result<ID3D11ShaderResourceView, i32> {
    let (slot_index, generation) = atlas_handle_parts(atlas)?;
    let mut state = lock_global();
    if slot_index >= state.slots.len() {
        return Err(atlas_status::ERR_BAD_HANDLE);
    }
    let slot = &state.slots[slot_index];
    if !slot.occupied || slot.slot_generation != generation {
        return Err(atlas_status::ERR_STALE_HANDLE);
    }
    let atlas_state = slot.atlas.as_ref().ok_or(atlas_status::ERR_STALE_HANDLE)?;
    let list = match kind {
        atlas_texture_kind::MONOCHROME => &atlas_state.monochrome_textures,
        atlas_texture_kind::POLYCHROME => &atlas_state.polychrome_textures,
        atlas_texture_kind::SUBPIXEL => &atlas_state.subpixel_textures,
        _ => return Err(atlas_status::ERR_BAD_VALUE),
    };
    let texture = list
        .textures
        .get(index as usize)
        .and_then(|slot| slot.as_ref())
        .ok_or(atlas_status::ERR_NOT_FOUND)?;
    if let Some(view) = texture.view.as_ref() {
        return Ok(view.clone());
    }
    // Create the view lazily (the pin creates it at texture creation;
    // both pin the texture for the view's lifetime). The device handle
    // is cloned (AddRef) so the creation runs without holding the
    // atlas-state borrow.
    let device = atlas_state.device.clone();
    let texture_handle = texture.texture.clone();
    let mut view = None;
    unsafe {
        device
            .CreateShaderResourceView(&texture_handle, None, Some(&mut view))
            .map_err(|e| e.code().0)?;
    }
    let view = view.ok_or(atlas_status::ERR_DEVICE)?;
    // Cache the view on the texture (a fresh mutable borrow through the
    // same slot).
    let slot = state.slots.get_mut(slot_index).ok_or(atlas_status::ERR_BAD_HANDLE)?;
    let atlas_state = slot.atlas.as_mut().ok_or(atlas_status::ERR_STALE_HANDLE)?;
    let list = match kind {
        atlas_texture_kind::MONOCHROME => &mut atlas_state.monochrome_textures,
        atlas_texture_kind::POLYCHROME => &mut atlas_state.polychrome_textures,
        atlas_texture_kind::SUBPIXEL => &mut atlas_state.subpixel_textures,
        _ => return Err(atlas_status::ERR_BAD_VALUE),
    };
    if let Some(texture) = list.textures.get_mut(index as usize).and_then(|slot| slot.as_mut()) {
        texture.view = Some(view.clone());
    }
    Ok(view)
}

unsafe fn atlas_dispose_body(handle: u64) -> i32 {
    let mut state = lock_global();
    let (slot_index, _atlas) = match resolve_slot(&mut state, handle) {
        Ok(resolved) => resolved,
        Err(atlas_status::ERR_STALE_HANDLE) => return atlas_status::ERR_STALE_HANDLE,
        Err(code) => return code,
    };
    // Dropping the atlas state releases every texture (COM
    // refcounts) and every tile record — the pin's Drop path.
    state.slots[slot_index].atlas = None;
    state.slots[slot_index].occupied = false;
    atlas_status::OK
}

unsafe fn atlas_insert_body(
    handle: u64,
    key: *const GpuiGoAtlasKeyRecord,
    width: i32,
    height: i32,
    bytes: *const u8,
    byte_count: u32,
    out_tile: *mut GpuiGoAtlasTileRecord,
) -> i32 {
    unsafe {
        if out_tile.is_null() || key.is_null() {
            return atlas_status::ERR_NULL_ARG;
        }
        let out_tile = &mut *out_tile;
        *out_tile = GpuiGoAtlasTileRecord::default();
        if byte_count > 0 && bytes.is_null() {
            return atlas_status::ERR_NULL_ARG;
        }
        let record = &*key;
        let key = match key_from_record(record) {
            Ok(key) => key,
            Err(code) => return code,
        };

        // The pin's tile limit (directx_atlas.rs:86-89).
        if width > MAX_ATLAS_SIZE || height > MAX_ATLAS_SIZE {
            return atlas_status::ERR_BAD_VALUE;
        }

        let mut state = lock_global();
        let (_slot, atlas) = match resolve_slot(&mut state, handle) {
            Ok(resolved) => resolved,
            Err(code) => return code,
        };

        // The pinned map check first: a cached key returns its tile with
        // no upload and no new allocation (directx_atlas.rs:71-73).
        if let Some(tile) = atlas.tiles_by_key.get(&key) {
            *out_tile = *tile;
            return atlas_status::OK;
        }

        // Validate before allocation: a rejected bitmap must never leave
        // a cached, uninitialized tile (directx_atlas.rs:78-85).
        if let Err(code) = validate_upload(key.texture_kind(), width, height, byte_count) {
            return code;
        }
        let byte_slice: &[u8] = if byte_count == 0 {
            &[]
        } else {
            std::slice::from_raw_parts(bytes, byte_count as usize)
        };

        let allocation = match atlas.allocate(width, height, key.texture_kind()) {
            Ok(Some(allocation)) => allocation,
            Ok(None) => return atlas_status::ERR_DEVICE,
            Err(code) => return code,
        };
        let tile = GpuiGoAtlasTileRecord {
            texture_index: allocation.0,
            texture_kind: allocation.1,
            tile_id: allocation.2,
            padding: 0,
            bounds_x: allocation.3,
            bounds_y: allocation.4,
            bounds_w: width,
            bounds_h: height,
            generation: atlas.generation,
            record_size: core::mem::size_of::<GpuiGoAtlasTileRecord>() as u32,
        };

        atlas.upload(&tile, byte_slice);
        atlas.tiles_by_key.insert(key, tile);
        *out_tile = tile;
        atlas_status::OK
    }
}

unsafe fn atlas_remove_body(handle: u64, key: *const GpuiGoAtlasKeyRecord) -> i32 {
    unsafe {
        if key.is_null() {
            return atlas_status::ERR_NULL_ARG;
        }
        let record = &*key;
        let key = match key_from_record(record) {
            Ok(key) => key,
            Err(code) => return code,
        };
        let mut state = lock_global();
        let (_slot, atlas) = match resolve_slot(&mut state, handle) {
            Ok(resolved) => resolved,
            Err(code) => return code,
        };
        atlas.remove(&key);
        atlas_status::OK
    }
}

unsafe fn atlas_query_body(
    handle: u64,
    key: *const GpuiGoAtlasKeyRecord,
    out_tile: *mut GpuiGoAtlasTileRecord,
) -> i32 {
    unsafe {
        if out_tile.is_null() || key.is_null() {
            return atlas_status::ERR_NULL_ARG;
        }
        let out_tile = &mut *out_tile;
        *out_tile = GpuiGoAtlasTileRecord::default();
        let record = &*key;
        let key = match key_from_record(record) {
            Ok(key) => key,
            Err(code) => return code,
        };
        let mut state = lock_global();
        let (_slot, atlas) = match resolve_slot(&mut state, handle) {
            Ok(resolved) => resolved,
            Err(code) => return code,
        };
        match atlas.tiles_by_key.get(&key) {
            Some(tile) => {
                *out_tile = *tile;
                atlas_status::OK
            }
            None => atlas_status::ERR_NOT_FOUND,
        }
    }
}

unsafe fn atlas_texture_count_body(handle: u64, kind: u32, out_count: *mut u32) -> i32 {
    unsafe {
        if out_count.is_null() {
            return atlas_status::ERR_NULL_ARG;
        }
        *out_count = 0;
        let textures = match kind {
            atlas_texture_kind::MONOCHROME | atlas_texture_kind::POLYCHROME
            | atlas_texture_kind::SUBPIXEL => kind,
            _ => return atlas_status::ERR_BAD_VALUE,
        };
        let mut state = lock_global();
        let (_slot, atlas) = match resolve_slot(&mut state, handle) {
            Ok(resolved) => resolved,
            Err(code) => return code,
        };
        let list = match textures {
            atlas_texture_kind::MONOCHROME => &atlas.monochrome_textures,
            atlas_texture_kind::POLYCHROME => &atlas.polychrome_textures,
            atlas_texture_kind::SUBPIXEL => &atlas.subpixel_textures,
            _ => unreachable!("validated above"),
        };
        *out_count = list.textures.iter().flatten().count() as u32;
        atlas_status::OK
    }
}

unsafe fn atlas_tile_count_body(handle: u64, out_count: *mut u32) -> i32 {
    unsafe {
        if out_count.is_null() {
            return atlas_status::ERR_NULL_ARG;
        }
        *out_count = 0;
        let mut state = lock_global();
        let (_slot, atlas) = match resolve_slot(&mut state, handle) {
            Ok(resolved) => resolved,
            Err(code) => return code,
        };
        *out_count = atlas.tiles_by_key.len() as u32;
        atlas_status::OK
    }
}

unsafe fn atlas_generation_body(handle: u64, out_generation: *mut u32) -> i32 {
    unsafe {
        if out_generation.is_null() {
            return atlas_status::ERR_NULL_ARG;
        }
        *out_generation = 0;
        let mut state = lock_global();
        let (_slot, atlas) = match resolve_slot(&mut state, handle) {
            Ok(resolved) => resolved,
            Err(code) => return code,
        };
        *out_generation = atlas.generation;
        atlas_status::OK
    }
}

unsafe fn atlas_notify_device_lost_body(handle: u64) -> i32 {
    // The pin's handle_device_lost (directx_atlas.rs:63-76): clear
    // the three texture pools and the tile map, then rebind to the
    // current device/context. The ABI additionally bumps the
    // generation carried by tile records.
    //
    // The renderer device handles are resolved BEFORE this service's
    // state lock is taken (the one nesting direction the services
    // allow is renderer-lock -> atlas-lock, which the draw path of
    // ticket16 relies on; taking the renderer lock under the atlas
    // lock here would invert it and could deadlock against a draw).
    let device = match crate::renderer::shared_device() {
        Ok(device) => device,
        Err(code) => return code,
    };
    let mut state = lock_global();
    let (_slot, atlas) = match resolve_slot(&mut state, handle) {
        Ok(resolved) => resolved,
        Err(code) => return code,
    };
    atlas.device = device.device;
    atlas.device_context = device.context;
    atlas.monochrome_textures = AtlasTextureList::default();
    atlas.polychrome_textures = AtlasTextureList::default();
    atlas.subpixel_textures = AtlasTextureList::default();
    atlas.tiles_by_key.clear();
    atlas.generation = atlas
        .generation
        .checked_add(1)
        .filter(|generation| *generation != 0)
        .unwrap_or(1);
    atlas_status::OK
}

// ---------------------------------------------------------------------------
// ABI exports (panic boundaries)
// ---------------------------------------------------------------------------

macro_rules! atlas_export {
    ($name:ident($($arg:ident: $ty:ty),*) -> $ret:ty, $body:ident) => {
        unsafe extern "system" fn $name($($arg: $ty),*) -> $ret {
            let outcome = catch_unwind(AssertUnwindSafe(|| unsafe { $body($($arg),*) }));
            match outcome {
                Ok(code) => code,
                Err(payload) => {
                    drop(payload);
                    atlas_status::ERR_PANIC
                }
            }
        }
    };
}

atlas_export!(atlas_create(out_handle: *mut u64) -> i32, atlas_create_body);
atlas_export!(atlas_dispose(handle: u64) -> i32, atlas_dispose_body);
atlas_export!(
    atlas_insert(
        handle: u64,
        key: *const GpuiGoAtlasKeyRecord,
        width: i32,
        height: i32,
        bytes: *const u8,
        byte_count: u32,
        out_tile: *mut GpuiGoAtlasTileRecord
    ) -> i32,
    atlas_insert_body
);
atlas_export!(
    atlas_remove(handle: u64, key: *const GpuiGoAtlasKeyRecord) -> i32,
    atlas_remove_body
);
atlas_export!(
    atlas_query(
        handle: u64,
        key: *const GpuiGoAtlasKeyRecord,
        out_tile: *mut GpuiGoAtlasTileRecord
    ) -> i32,
    atlas_query_body
);
atlas_export!(
    atlas_texture_count(handle: u64, kind: u32, out_count: *mut u32) -> i32,
    atlas_texture_count_body
);
atlas_export!(atlas_tile_count(handle: u64, out_count: *mut u32) -> i32, atlas_tile_count_body);
atlas_export!(atlas_generation(handle: u64, out_generation: *mut u32) -> i32, atlas_generation_body);
atlas_export!(
    atlas_notify_device_lost(handle: u64) -> i32,
    atlas_notify_device_lost_body
);

/// Test-only panic probe.
unsafe extern "system" fn atlas_panic_probe() -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| {
        panic!("atlas service panic probe (contained by design)");
    }));
    match outcome {
        Ok(()) => atlas_status::OK,
        Err(payload) => {
            drop(payload);
            atlas_status::ERR_PANIC
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn create_atlas() -> Option<u64> {
        let mut handle = 0u64;
        let code = unsafe { atlas_create_body(&mut handle) };
        if code != atlas_status::OK {
            return None;
        }
        Some(handle)
    }

    fn glyph_key(font_id: u64, glyph_id: u32, format: u32) -> GpuiGoAtlasKeyRecord {
        let mut key = GpuiGoAtlasKeyRecord::default();
        key.font_id = font_id;
        key.glyph_id = glyph_id;
        key.font_size_bits = 24.0f32.to_bits();
        key.scale_bits = 1.0f32.to_bits();
        key.format = format;
        key
    }

    fn insert_tile(
        handle: u64,
        key: &GpuiGoAtlasKeyRecord,
        width: i32,
        height: i32,
    ) -> GpuiGoAtlasTileRecord {
        let channels = match key.format {
            raster_format::ALPHA_MASK => 1,
            _ => 4,
        };
        let byte_count = (width * height * channels) as u32;
        let bytes = vec![0u8; byte_count as usize];
        let mut tile = GpuiGoAtlasTileRecord::default();
        let code = unsafe {
            atlas_insert_body(
                handle,
                key as *const _,
                width,
                height,
                bytes.as_ptr(),
                byte_count,
                &mut tile,
            )
        };
        assert_eq!(code, atlas_status::OK, "insert failed");
        tile
    }

    /// The pin's own test (directx_atlas.rs:400-434, adapted): removing a
    /// big tile deallocates its space so another big tile reuses the same
    /// texture instead of growing the pool.
    #[test]
    fn test_remove_deallocates_tile_space_for_reuse() {
        let Some(atlas) = create_atlas() else {
            return;
        };

        let small = glyph_key(1, 1, raster_format::BGRA_COLOR);
        let big_a = glyph_key(2, 2, raster_format::BGRA_COLOR);
        let big_b = glyph_key(3, 3, raster_format::BGRA_COLOR);

        let keeper = insert_tile(atlas, &small, 64, 64);
        let tile_a = insert_tile(atlas, &big_a, 700, 700);
        assert_eq!(keeper.texture_index, tile_a.texture_index);

        unsafe { atlas_remove_body(atlas, &big_a as *const _) };

        let tile_b = insert_tile(atlas, &big_b, 700, 700);
        assert_eq!(tile_b.texture_index, keeper.texture_index);

        unsafe { atlas_dispose_body(atlas) };
    }

    #[test]
    fn insert_is_idempotent_per_key() {
        let Some(atlas) = create_atlas() else {
            return;
        };
        let key = glyph_key(9, 9, raster_format::ALPHA_MASK);
        let first = insert_tile(atlas, &key, 20, 30);
        let bytes = vec![7u8; 20 * 30];
        let mut second = GpuiGoAtlasTileRecord::default();
        let code = unsafe {
            atlas_insert_body(
                atlas,
                &key as *const _,
                20,
                30,
                bytes.as_ptr(),
                (20 * 30) as u32,
                &mut second,
            )
        };
        assert_eq!(code, atlas_status::OK);
        assert_eq!(first.tile_id, second.tile_id);
        assert_eq!(first.texture_index, second.texture_index);
        assert_eq!(first.bounds_x, second.bounds_x);
        assert_eq!(first.bounds_y, second.bounds_y);

        // One texture in the monochrome pool, one live tile.
        let mut count = 0u32;
        unsafe { atlas_texture_count_body(atlas, atlas_texture_kind::MONOCHROME, &mut count) };
        assert_eq!(count, 1);
        unsafe { atlas_tile_count_body(atlas, &mut count) };
        assert_eq!(count, 1);

        unsafe { atlas_dispose_body(atlas) };
    }

    #[test]
    fn pools_are_separated_by_kind() {
        let Some(atlas) = create_atlas() else {
            return;
        };
        let mono = insert_tile(atlas, &glyph_key(1, 1, raster_format::ALPHA_MASK), 10, 10);
        let poly = insert_tile(atlas, &glyph_key(2, 2, raster_format::BGRA_COLOR), 10, 10);
        let sub = insert_tile(atlas, &glyph_key(3, 3, raster_format::BGRA_SUBPIXEL_MASK), 10, 10);
        assert_eq!(mono.texture_kind, atlas_texture_kind::MONOCHROME);
        assert_eq!(poly.texture_kind, atlas_texture_kind::POLYCHROME);
        assert_eq!(sub.texture_kind, atlas_texture_kind::SUBPIXEL);

        let mut count = 0u32;
        for kind in [
            atlas_texture_kind::MONOCHROME,
            atlas_texture_kind::POLYCHROME,
            atlas_texture_kind::SUBPIXEL,
        ] {
            unsafe { atlas_texture_count_body(atlas, kind, &mut count) };
            assert_eq!(count, 1, "one texture per kind pool");
        }
        unsafe { atlas_dispose_body(atlas) };
    }

    #[test]
    fn malformed_uploads_are_rejected_without_side_effects() {
        let Some(atlas) = create_atlas() else {
            return;
        };
        // Byte count mismatch for the monochrome kind.
        let key = glyph_key(1, 1, raster_format::ALPHA_MASK);
        let mut tile = GpuiGoAtlasTileRecord::default();
        let code = unsafe {
            atlas_insert_body(
                atlas,
                &key as *const _,
                10,
                10,
                [0u8; 4].as_ptr(),
                4,
                &mut tile,
            )
        };
        assert_eq!(code, atlas_status::ERR_BAD_VALUE);
        // Nothing was published.
        let mut count = 0u32;
        unsafe { atlas_tile_count_body(atlas, &mut count) };
        assert_eq!(count, 0);
        // Oversized tile.
        let code = unsafe {
            atlas_insert_body(atlas, &key as *const _, 20000, 10, [].as_ptr(), 0, &mut tile)
        };
        assert_eq!(code, atlas_status::ERR_BAD_VALUE);
        // The svg/image key kinds are later tickets.
        let mut svg_key = glyph_key(1, 1, raster_format::ALPHA_MASK);
        svg_key.kind = 1;
        let code = unsafe {
            atlas_insert_body(atlas, &svg_key as *const _, 10, 10, [0u8; 100].as_ptr(), 100, &mut tile)
        };
        assert_eq!(code, atlas_status::ERR_BAD_VALUE);
        unsafe { atlas_dispose_body(atlas) };
    }

    #[test]
    fn query_and_remove_return_the_pinned_outcomes() {
        let Some(atlas) = create_atlas() else {
            return;
        };
        let key = glyph_key(4, 4, raster_format::BGRA_SUBPIXEL_MASK);
        let inserted = insert_tile(atlas, &key, 8, 8);
        let mut queried = GpuiGoAtlasTileRecord::default();
        let code = unsafe { atlas_query_body(atlas, &key as *const _, &mut queried) };
        assert_eq!(code, atlas_status::OK);
        assert_eq!(queried.tile_id, inserted.tile_id);

        unsafe { atlas_remove_body(atlas, &key as *const _) };
        let code = unsafe { atlas_query_body(atlas, &key as *const _, &mut queried) };
        assert_eq!(code, atlas_status::ERR_NOT_FOUND);

        // Removing an absent key is a no-op (the pin's early return).
        let code = unsafe { atlas_remove_body(atlas, &key as *const _) };
        assert_eq!(code, atlas_status::OK);
        unsafe { atlas_dispose_body(atlas) };
    }

    #[test]
    fn device_loss_invalidates_tiles_and_bumps_generation() {
        let Some(atlas) = create_atlas() else {
            return;
        };
        let key = glyph_key(5, 5, raster_format::ALPHA_MASK);
        let inserted = insert_tile(atlas, &key, 12, 12);
        let mut generation = 0u32;
        unsafe { atlas_generation_body(atlas, &mut generation) };
        assert_eq!(generation, 1);
        assert_eq!(inserted.generation, 1);

        unsafe { atlas_notify_device_lost_body(atlas) };
        unsafe { atlas_generation_body(atlas, &mut generation) };
        assert_eq!(generation, 2);

        let mut tile = GpuiGoAtlasTileRecord::default();
        let code = unsafe { atlas_query_body(atlas, &key as *const _, &mut tile) };
        assert_eq!(code, atlas_status::ERR_NOT_FOUND);

        // The cleared pools accept a fresh insert (same key, new tile).
        let reinserted = insert_tile(atlas, &key, 12, 12);
        assert_eq!(reinserted.generation, 2);
        unsafe { atlas_dispose_body(atlas) };
    }

    #[test]
    fn stale_and_malformed_handles_are_typed_errors() {
        let mut handle = 0u64;
        let code = unsafe { atlas_create_body(&mut handle) };
        assert_eq!(code, atlas_status::OK);
        let disposed = handle;
        unsafe { atlas_dispose_body(disposed) };

        // Stale after dispose.
        let mut tile = GpuiGoAtlasTileRecord::default();
        let key = glyph_key(1, 1, raster_format::ALPHA_MASK);
        let code = unsafe { atlas_query_body(disposed, &key as *const _, &mut tile) };
        assert_eq!(code, atlas_status::ERR_STALE_HANDLE);

        // Malformed (out-of-range slot).
        let bad = make_atlas_handle(MAX_ATLAS_SLOTS as usize, 1);
        let code = unsafe { atlas_query_body(bad, &key as *const _, &mut tile) };
        assert_eq!(code, atlas_status::ERR_BAD_HANDLE);

        // Null arguments.
        let code = unsafe { atlas_query_body(disposed, core::ptr::null(), &mut tile) };
        assert_eq!(code, atlas_status::ERR_NULL_ARG);
        let code = unsafe { atlas_create_body(core::ptr::null_mut()) };
        assert_eq!(code, atlas_status::ERR_NULL_ARG);
    }

    #[test]
    fn table_self_checks() {
        assert_eq!(ATLAS_TABLE.service_version, GPUI_GO_ATLAS_SERVICE_VERSION);
        assert!(ATLAS_TABLE.create.is_some());
        assert!(ATLAS_TABLE.dispose.is_some());
        assert!(ATLAS_TABLE.insert.is_some());
        assert!(ATLAS_TABLE.remove.is_some());
        assert!(ATLAS_TABLE.query.is_some());
        assert!(ATLAS_TABLE.texture_count.is_some());
        assert!(ATLAS_TABLE.tile_count.is_some());
        assert!(ATLAS_TABLE.generation.is_some());
        assert!(ATLAS_TABLE.notify_device_lost.is_some());
        assert!(ATLAS_TABLE.panic_probe.is_some());
        assert_eq!(ATLAS_TABLE.default_atlas_size, 1024);
        assert_eq!(ATLAS_TABLE.max_atlas_size, 16384);
        assert_eq!(ATLAS_TABLE.max_atlas_slots, MAX_ATLAS_SLOTS);
    }

    #[test]
    fn panic_probe_reports_containment() {
        assert_eq!(unsafe { atlas_panic_probe() }, atlas_status::ERR_PANIC);
    }
}
