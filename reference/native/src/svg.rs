//! gpui-go native SVG service (ticket19, reserved slot 8).
//!
//! The pinned SVG surface of the gpui-CE reference: the `resvg`/
//! `usvg` 0.48.1 stack (workspace `resvg = "0.48.1"` with features
//! text/system-fonts/memmap-fonts/raster-images, `usvg = "0.48.1"`
//! default-features off — already resolved in the workspace lock via
//! the gpui-ce path dependency; this service adds NO new crate),
//! driven exactly through the public paths
//! `crates/gpui/src/svg_renderer.rs` exposes:
//!
//! * `SvgRenderer::new(Arc<AssetRegistry>)` — the pinned constructor
//!   whose font resolver clones the system font database on first
//!   use, loads the bundled font list from the asset registry
//!   (`load_bundled_fonts`: the two pinned TTF paths), fixes the
//!   generic families (`fix_generic_font_families`), and adds the
//!   emoji-presentation fallback (`select_emoji_font` over Segoe UI
//!   Emoji/Symbol on Windows) — all reused verbatim by wrapping.
//! * `SvgRenderer::parse_svg` / `render_parsed` — parsing resolves
//!   fonts and converts text to paths; rasterization applies the
//!   pin's sizing modes (`SvgSize::Size` aspect-preserving,
//!   `ExactSize`, `ScaleFactor` with `SMOOTH_SVG_SCALE_FACTOR = 2`),
//!   the 8192-pixel `MAX_SIZE` clamp, and the premultiplied-RGBA →
//!   BGRA pixel swap, returning a one-frame BGRA `RenderImage`.
//! * `render_alpha_mask` is `pub(crate)` in the pin, so this service
//!   re-implements its 10-line body (rasterize at `SvgSize::Size`,
//!   collect the alpha channel) against the public `render_parsed`,
//!   cited at the site.
//!
//! Font assets: the pin's renderer reads its bundled fonts from the
//! AssetRegistry synchronously. The Go port cannot allow a native
//! worker to call back into Go, so this service owns the registry:
//! the pinned font-asset paths are reported as owned data
//! (`svg_font_asset_count`/`svg_font_asset_path`) for Go task
//! orchestration, `svg_add_font` pushes loaded bytes in, and
//! `svg_has_font_asset` reports the missing set (the
//! NeedsFontAssets request of the runtime ownership contract). A
//! font added after the first parse rebuilds the renderer lazily on
//! the next parse — the pin's enriched font database is a `OnceLock`
//! snapshot inside the resolver, so this is the port's "renderer
//! font snapshot/missing-cache lifetime" adaptation; previously
//! parsed trees stay valid (text already resolved to paths).
//!
//! ABI shape (see the crate docs and IMAGE_ABI.md's conventions):
//! fixed-width `#[repr(C)]` records, no Rust slices/strings/enums
//! crossing the boundary, caller-owned buffers with the capacity
//! protocol, every export containing panics with `catch_unwind`, and
//! a slot registry for parsed trees mirroring the text/image
//! services (handle = `(slot index << 20) | slot generation`, stale
//! after dispose or slot reuse, capacity [`MAX_SVG_SLOTS`]).

use std::panic::{catch_unwind, AssertUnwindSafe};
use std::sync::{Arc, LazyLock, Mutex, MutexGuard};

use gpui::{AssetEntry, AssetRegistry, ParsedSvg, SharedString, SvgRenderer, SvgSize};

// ---------------------------------------------------------------------------
// Constants, status codes, render modes
// ---------------------------------------------------------------------------

/// Live parsed-tree slot bound (the text/image services' slot bound,
/// mirrored).
pub const MAX_SVG_SLOTS: u32 = 256;

/// Maximum accepted SVG byte length (caller memory is copied into the
/// parse; the bound keeps the native work bounded).
pub const MAX_SVG_BYTES: u32 = 1 << 28;

/// The pinned smooth-scale factor applied to `ScaleFactor` renders
/// (svg_renderer.rs `SMOOTH_SVG_SCALE_FACTOR`): the SVG rasterizes at
/// twice the requested scale and reports the factor so callers divide
/// back for logical sizing.
pub const SVG_SMOOTH_SCALE_FACTOR: f32 = 2.0;

/// SVG service status codes. 0 is success; negative values are
/// caller/argument errors; 100+ are internal failures.
pub mod svg_status {
    /// Success.
    pub const OK: i32 = 0;
    /// The SVG handle encoded a past generation (disposed or slot
    /// reused since).
    pub const ERR_STALE_HANDLE: i32 = -1;
    /// The handle never existed or is malformed (out-of-range slot or
    /// a never-issued generation).
    pub const ERR_BAD_HANDLE: i32 = -2;
    /// A required pointer argument was null.
    pub const ERR_NULL_ARG: i32 = -3;
    /// An invalid length, capacity, mode tag, or font-asset index; a
    /// duplicate font-asset path.
    pub const ERR_BAD_VALUE: i32 = -4;
    /// `usvg` rejected the bytes or the rasterization failed
    /// (the pin's `usvg::Error`).
    pub const ERR_PARSE: i32 = -5;
    /// A dump capacity was too small; `out_needed` carries the required
    /// byte count.
    pub const ERR_CAPACITY: i32 = -6;
    /// The live SVG-handle capacity ([`MAX_SVG_SLOTS`]) is exhausted.
    pub const ERR_HANDLE_LIMIT: i32 = -7;
    /// A native panic was contained by `catch_unwind` during this call.
    pub const ERR_PANIC: i32 = 101;
}

/// Render sizing modes (u32 tags on the ABI; one per pinned
/// `SvgSize` construction the service accepts).
pub mod svg_mode {
    /// `SvgSize::Size` — a width in device pixels, aspect preserved.
    pub const SIZE: u32 = 1;
    /// `SvgSize::ExactSize` — exact width and height in device pixels.
    pub const EXACT_SIZE: u32 = 2;
    /// `SvgSize::ScaleFactor` — a logical scaling factor (the smooth
    /// 2x applies and is reported back).
    pub const SCALE_FACTOR: u32 = 3;
}

/// The pinned bundled-font asset paths the renderer's resolver
/// consults (`svg_renderer.rs::load_bundled_fonts`): reported as
/// owned data so the Go side can orchestrate their loading from the
/// application's asset registry exactly once.
pub const SVG_FONT_ASSET_PATHS: [&str; 2] = [
    "fonts/ibm-plex-sans/IBMPlexSans-Regular.ttf",
    "fonts/lilex/Lilex-Regular.ttf",
];

// ---------------------------------------------------------------------------
// The render info record
// ---------------------------------------------------------------------------

/// One rasterization result: the pixel dimensions of the BGRA buffer
/// and the scale factor the pinned render path applied (2.0 for
/// `ScaleFactor` renders — the smooth-scale division factor; 1.0 for
/// the explicit-size modes).
#[repr(C)]
pub struct GpuiGoSvgRenderInfo {
    /// Rendered width in pixels (BGRA row stride is `width * 4`).
    pub width: u32,
    /// Rendered height in pixels.
    pub height: u32,
    /// The applied scale factor (svg_renderer.rs `image.scale_factor`).
    pub scale_factor: f32,
}

// ---------------------------------------------------------------------------
// Handle registry and service state
// ---------------------------------------------------------------------------

/// One parsed SVG tree occupying a slot.
struct SvgSlot {
    /// Slot generation of the last tree that occupied this slot
    /// (persists while vacant so disposed handles stay stale after slot
    /// reuse, the text/image services' rule).
    slot_generation: u32,
    /// Whether the slot currently holds a live tree.
    occupied: bool,
    /// The parsed tree (`SvgRenderer::parse_svg` output).
    tree: Option<ParsedSvg>,
}

/// The SVG service state behind one mutex.
struct SvgServiceState {
    slots: Vec<SvgSlot>,
    /// Font assets pushed by the Go side: (path, bytes). The registry
    /// insert preserves push order; duplicates are rejected.
    fonts: Vec<(String, Vec<u8>)>,
    /// The live renderer (rebuilt lazily when fonts were added after
    /// the last build). `None` until the first use.
    renderer: Option<Arc<SvgRenderer>>,
    /// How many of `fonts` were baked into the live renderer.
    renderer_font_count: usize,
}

impl SvgServiceState {
    fn new() -> SvgServiceState {
        SvgServiceState { slots: Vec::new(), fonts: Vec::new(), renderer: None, renderer_font_count: 0 }
    }
}

/// The process-wide SVG service state.
static SVG_STATE: LazyLock<Mutex<SvgServiceState>> =
    LazyLock::new(|| Mutex::new(SvgServiceState::new()));

/// Locks the state, poisoning-transparently (no callback can run under
/// the lock, so a panic while holding it leaves no partial state; the
/// next lock recovers — the text service's rule).
fn lock_state() -> MutexGuard<'static, SvgServiceState> {
    match SVG_STATE.lock() {
        Ok(guard) => guard,
        Err(poisoned) => poisoned.into_inner(),
    }
}

/// Encodes (slot, generation) into the u64 handle.
fn encode_handle(slot: u32, generation: u32) -> u64 {
    ((slot as u64) << 20) | (generation as u64)
}

/// Decodes a handle into (slot, generation).
fn decode_handle(handle: u64) -> (u32, u32) {
    ((handle >> 20) as u32, (handle as u32) & 0x000F_FFFF)
}

/// The maximum generation that still round-trips through the encoding
/// (20 bits, the text/image services' layout).
const MAX_GENERATION: u32 = 0x000F_FFFF;

/// Resolves a handle to its live tree, or the stale/bad status.
fn resolve_tree<'a>(
    state: &'a mut SvgServiceState,
    handle: u64,
) -> Result<&'a mut ParsedSvg, i32> {
    let (slot, generation) = decode_handle(handle);
    if slot as usize >= state.slots.len() {
        return Err(svg_status::ERR_BAD_HANDLE);
    }
    let entry = &mut state.slots[slot as usize];
    if !entry.occupied || entry.slot_generation != generation {
        return Err(svg_status::ERR_STALE_HANDLE);
    }
    entry
        .tree
        .as_mut()
        .ok_or(svg_status::ERR_BAD_HANDLE)
}

/// Allocates a slot for one tree, reporting the new handle.
fn insert_tree(state: &mut SvgServiceState, tree: ParsedSvg) -> Result<u64, i32> {
    // First fit (the image service's allocation policy).
    for (index, entry) in state.slots.iter_mut().enumerate() {
        if !entry.occupied {
            let generation = entry.slot_generation.wrapping_add(1) & MAX_GENERATION;
            if generation == 0 {
                // Wrapped to the never-issued zero generation of a
                // freshly created slot; bump once more.
                entry.slot_generation = 1;
            } else {
                entry.slot_generation = generation;
            }
            entry.occupied = true;
            entry.tree = Some(tree);
            return Ok(encode_handle(index as u32, entry.slot_generation));
        }
    }
    if state.slots.len() >= MAX_SVG_SLOTS as usize {
        return Err(svg_status::ERR_HANDLE_LIMIT);
    }
    state.slots.push(SvgSlot {
        slot_generation: 1,
        occupied: true,
        tree: Some(tree),
    });
    Ok(encode_handle((state.slots.len() - 1) as u32, 1))
}

// ---------------------------------------------------------------------------
// Font assets (the NeedsFontAssets seam)
// ---------------------------------------------------------------------------

fn font_asset_count_body() -> i32 {
    SVG_FONT_ASSET_PATHS.len() as i32
}

/// The pinned path list is static UTF-8; this returns a borrowed
/// pointer into it (valid for the process lifetime, like the format
/// tags the image service reports).
fn font_asset_path_body(index: u32, out_ptr: *mut *const u8, out_len: *mut u32) -> i32 {
    if out_ptr.is_null() || out_len.is_null() {
        return svg_status::ERR_NULL_ARG;
    }
    if index as usize >= SVG_FONT_ASSET_PATHS.len() {
        return svg_status::ERR_BAD_VALUE;
    }
    let path = SVG_FONT_ASSET_PATHS[index as usize];
    unsafe {
        *out_ptr = path.as_ptr();
        *out_len = path.len() as u32;
    }
    svg_status::OK
}

fn has_font_asset_body(path_ptr: *const u8, path_len: u32) -> i32 {
    if path_ptr.is_null() || path_len == 0 {
        return svg_status::ERR_NULL_ARG;
    }
    let path = match std::str::from_utf8(unsafe { std::slice::from_raw_parts(path_ptr, path_len as usize) }) {
        Ok(s) => s,
        Err(_) => return svg_status::ERR_BAD_VALUE,
    };
    let state = lock_state();
    if state.fonts.iter().any(|(p, _)| p == path) {
        1
    } else {
        0
    }
}

fn add_font_body(path_ptr: *const u8, path_len: u32, bytes_ptr: *const u8, bytes_len: u32) -> i32 {
    if path_ptr.is_null() || bytes_ptr.is_null() || path_len == 0 || bytes_len == 0 {
        return svg_status::ERR_NULL_ARG;
    }
    if bytes_len > MAX_SVG_BYTES {
        return svg_status::ERR_BAD_VALUE;
    }
    let path = match std::str::from_utf8(unsafe { std::slice::from_raw_parts(path_ptr, path_len as usize) }) {
        Ok(s) => s,
        Err(_) => return svg_status::ERR_BAD_VALUE,
    };
    if path.is_empty() || path.len() > 1024 {
        return svg_status::ERR_BAD_VALUE;
    }
    let bytes = unsafe { std::slice::from_raw_parts(bytes_ptr, bytes_len as usize) }.to_vec();
    let mut state = lock_state();
    if state.fonts.iter().any(|(p, _)| p == path) {
        // The registry's DuplicateAssetPath rule (assets.rs insert).
        return svg_status::ERR_BAD_VALUE;
    }
    state.fonts.push((path.to_string(), bytes));
    // The live renderer's enriched font database is a OnceLock
    // snapshot (svg_renderer.rs); the next parse rebuilds against the
    // extended font set — the port's font-snapshot lifetime rule.
    svg_status::OK
}

// ---------------------------------------------------------------------------
// Parse and render
// ---------------------------------------------------------------------------

/// Builds (or rebuilds) the renderer against the accumulated font
/// set. The pinned constructor loads system fonts lazily on first
/// resolution, so the rebuild cost is bounded to the registry copy.
fn ensure_renderer(state: &mut SvgServiceState) -> Arc<SvgRenderer> {
    if let Some(renderer) = &state.renderer {
        if state.renderer_font_count == state.fonts.len() {
            return renderer.clone();
        }
    }
    let mut registry = AssetRegistry::default();
    for (path, bytes) in &state.fonts {
        // PreLoaded entries (the registry's Cow<'static, [u8]> shape):
        // load_bundled_fonts reads exactly the pinned paths from this
        // registry inside SvgRenderer's resolver.
        let _ = registry.insert(
            SharedString::from(path.as_str()),
            AssetEntry::PreLoaded(std::borrow::Cow::Owned(bytes.clone())),
        );
    }
    let renderer = Arc::new(SvgRenderer::new(Arc::new(registry)));
    state.renderer = Some(renderer.clone());
    state.renderer_font_count = state.fonts.len();
    renderer
}

fn parse_body(bytes: *const u8, len: u32, out_handle: *mut u64) -> i32 {
    if bytes.is_null() || out_handle.is_null() {
        return svg_status::ERR_NULL_ARG;
    }
    if len == 0 || len > MAX_SVG_BYTES {
        return svg_status::ERR_BAD_VALUE;
    }
    let data = unsafe { std::slice::from_raw_parts(bytes, len as usize) };
    let mut state = lock_state();
    let renderer = ensure_renderer(&mut state);
    // parse_svg copies what it needs (usvg::Tree::from_data); release
    // the lock for the parse so concurrent callers do not serialize on
    // the registry alone.
    drop(state);
    match renderer.parse_svg(data) {
        Ok(tree) => {
            let mut state = lock_state();
            match insert_tree(&mut state, tree) {
                Ok(handle) => {
                    unsafe { *out_handle = handle };
                    svg_status::OK
                }
                Err(code) => code,
            }
        }
        Err(_) => svg_status::ERR_PARSE,
    }
}

fn dispose_body(handle: u64) -> i32 {
    let mut state = lock_state();
    let (slot, generation) = decode_handle(handle);
    if slot as usize >= state.slots.len() {
        return svg_status::ERR_BAD_HANDLE;
    }
    let entry = &mut state.slots[slot as usize];
    if !entry.occupied || entry.slot_generation != generation {
        return svg_status::ERR_STALE_HANDLE;
    }
    entry.occupied = false;
    entry.tree = None;
    svg_status::OK
}

/// Rasterizes one tree into the caller's BGRA buffer (the capacity
/// protocol: on `ERR_CAPACITY`, `out_needed` carries the required byte
/// count and nothing is written).
fn render_body(
    handle: u64,
    mode: u32,
    width: u32,
    height: u32,
    scale: f32,
    buf: *mut u8,
    capacity: u32,
    out_needed: *mut u32,
    out_info: *mut GpuiGoSvgRenderInfo,
) -> i32 {
    if buf.is_null() || out_needed.is_null() || out_info.is_null() {
        // The capacity protocol's query call passes a null buffer with
        // zero capacity; only that combination is a valid query.
        if !(buf.is_null() && capacity == 0) {
            return svg_status::ERR_NULL_ARG;
        }
    }
    if out_needed.is_null() || out_info.is_null() {
        return svg_status::ERR_NULL_ARG;
    }
    if !scale.is_finite() || scale <= 0.0 || (mode != svg_mode::SCALE_FACTOR && (width == 0 || height == 0))
    {
        return svg_status::ERR_BAD_VALUE;
    }
    let mut state = lock_state();
    let renderer = ensure_renderer(&mut state);
    let tree = match resolve_tree(&mut state, handle) {
        Ok(tree) => tree,
        Err(code) => return code,
    };
    let size = match mode {
        svg_mode::SIZE => SvgSize::Size(gpui::Size {
            width: gpui::DevicePixels(width as i32),
            height: gpui::DevicePixels(height as i32),
        }),
        svg_mode::EXACT_SIZE => SvgSize::ExactSize(gpui::Size {
            width: gpui::DevicePixels(width as i32),
            height: gpui::DevicePixels(height as i32),
        }),
        svg_mode::SCALE_FACTOR => SvgSize::ScaleFactor(scale),
        _ => return svg_status::ERR_BAD_VALUE,
    };
    let applied_scale = if mode == svg_mode::SCALE_FACTOR {
        SVG_SMOOTH_SCALE_FACTOR
    } else {
        1.0
    };
    // render_parsed applies the pinned sizing, the 8192 clamp, the
    // premultiplied-RGBA→BGRA swap and returns a one-frame BGRA
    // RenderImage. The tree borrow ends here (render_parsed clones
    // nothing mutable; the Arc<RenderImage> is ours).
    let image = match renderer.render_parsed(tree, size) {
        Ok(image) => image,
        Err(_) => return svg_status::ERR_PARSE,
    };
    drop(state);
    let frame = match image.as_bytes(0) {
        Some(bytes) => bytes,
        None => return svg_status::ERR_PARSE,
    };
    let needed = frame.len() as u32;
    let w = image.size(0).width.0 as u32;
    let h = image.size(0).height.0 as u32;
    if capacity < needed || buf.is_null() {
        unsafe { *out_needed = needed };
        unsafe {
            *out_info = GpuiGoSvgRenderInfo { width: w, height: h, scale_factor: applied_scale };
        }
        return svg_status::ERR_CAPACITY;
    }
    unsafe {
        std::ptr::copy_nonoverlapping(frame.as_ptr(), buf, frame.len());
        *out_needed = needed;
        *out_info = GpuiGoSvgRenderInfo { width: w, height: h, scale_factor: applied_scale };
    }
    svg_status::OK
}

/// The pub(crate) `render_alpha_mask` adaptation (svg_renderer.rs):
/// rasterize at `SvgSize::Size` and collect the alpha channel.
fn render_alpha_mask_body(
    bytes: *const u8,
    len: u32,
    width: u32,
    height: u32,
    buf: *mut u8,
    capacity: u32,
    out_needed: *mut u32,
    out_info: *mut GpuiGoSvgRenderInfo,
) -> i32 {
    if bytes.is_null() || out_needed.is_null() || out_info.is_null() {
        return svg_status::ERR_NULL_ARG;
    }
    if buf.is_null() && capacity != 0 {
        return svg_status::ERR_NULL_ARG;
    }
    if len == 0 || len > MAX_SVG_BYTES || width == 0 || height == 0 {
        return svg_status::ERR_BAD_VALUE;
    }
    let data = unsafe { std::slice::from_raw_parts(bytes, len as usize) };
    let mut state = lock_state();
    let renderer = ensure_renderer(&mut state);
    drop(state);
    let tree = match renderer.parse_svg(data) {
        Ok(tree) => tree,
        Err(_) => return svg_status::ERR_PARSE,
    };
    let size = SvgSize::Size(gpui::Size {
        width: gpui::DevicePixels(width as i32),
        height: gpui::DevicePixels(height as i32),
    });
    let image = match renderer.render_parsed(&tree, size) {
        Ok(image) => image,
        Err(_) => return svg_status::ERR_PARSE,
    };
    let frame = match image.as_bytes(0) {
        Some(bytes) => bytes,
        None => return svg_status::ERR_PARSE,
    };
    // BGRA rows: alpha is every fourth byte (offset 3). The pin's
    // render_alpha_mask maps `pixmap.pixels()` alpha exactly.
    let needed = (frame.len() / 4) as u32;
    let w = image.size(0).width.0 as u32;
    let h = image.size(0).height.0 as u32;
    if capacity < needed || buf.is_null() {
        unsafe { *out_needed = needed };
        unsafe {
            *out_info = GpuiGoSvgRenderInfo { width: w, height: h, scale_factor: 1.0 };
        }
        return svg_status::ERR_CAPACITY;
    }
    unsafe {
        for (dst, px) in std::slice::from_raw_parts_mut(buf, needed as usize)
            .iter_mut()
            .zip(frame.chunks_exact(4))
        {
            *dst = px[3];
        }
        *out_needed = needed;
        *out_info = GpuiGoSvgRenderInfo { width: w, height: h, scale_factor: 1.0 };
    }
    svg_status::OK
}

// ---------------------------------------------------------------------------
// Panic containment and exports
// ---------------------------------------------------------------------------

macro_rules! svg_entry {
    ($body:expr) => {{
        let outcome = catch_unwind(AssertUnwindSafe(|| $body));
        match outcome {
            Ok(code) => code,
            Err(payload) => {
                // Drop the payload; nothing is retained. The state mutex
                // recovers from poisoning on the next lock (no callback can
                // run under it, so no partial state survives).
                drop(payload);
                svg_status::ERR_PANIC
            }
        }
    }};
}

unsafe extern "system" fn svg_font_asset_count() -> i32 {
    svg_entry!(font_asset_count_body())
}

unsafe extern "system" fn svg_font_asset_path(
    index: u32,
    out_ptr: *mut *const u8,
    out_len: *mut u32,
) -> i32 {
    svg_entry!(font_asset_path_body(index, out_ptr, out_len))
}

unsafe extern "system" fn svg_has_font_asset(path_ptr: *const u8, path_len: u32) -> i32 {
    svg_entry!(has_font_asset_body(path_ptr, path_len))
}

unsafe extern "system" fn svg_add_font(
    path_ptr: *const u8,
    path_len: u32,
    bytes_ptr: *const u8,
    bytes_len: u32,
) -> i32 {
    svg_entry!(add_font_body(path_ptr, path_len, bytes_ptr, bytes_len))
}

unsafe extern "system" fn svg_parse(bytes: *const u8, len: u32, out_handle: *mut u64) -> i32 {
    svg_entry!(parse_body(bytes, len, out_handle))
}

unsafe extern "system" fn svg_dispose(handle: u64) -> i32 {
    svg_entry!(dispose_body(handle))
}

unsafe extern "system" fn svg_render(
    handle: u64,
    mode: u32,
    width: u32,
    height: u32,
    scale: f32,
    buf: *mut u8,
    capacity: u32,
    out_needed: *mut u32,
    out_info: *mut GpuiGoSvgRenderInfo,
) -> i32 {
    svg_entry!(render_body(
        handle, mode, width, height, scale, buf, capacity, out_needed, out_info
    ))
}

unsafe extern "system" fn svg_render_alpha_mask(
    bytes: *const u8,
    len: u32,
    width: u32,
    height: u32,
    buf: *mut u8,
    capacity: u32,
    out_needed: *mut u32,
    out_info: *mut GpuiGoSvgRenderInfo,
) -> i32 {
    svg_entry!(render_alpha_mask_body(
        bytes, len, width, height, buf, capacity, out_needed, out_info
    ))
}

// ---------------------------------------------------------------------------
// The service table
// ---------------------------------------------------------------------------

/// The SVG service table: 8 function pointers then self-check,
/// capacity, mode-tag and font-surface scalars; size 112, alignment 8.
/// Installed in reserved slot 8 of the bootstrap table and advertised
/// with capability bit 9 (`svg-resvg-0-48`).
#[repr(C)]
pub struct GpuiGoSvgTable {
    /// `svg_font_asset_count`.
    pub font_asset_count: Option<unsafe extern "system" fn() -> i32>,
    /// `svg_font_asset_path`.
    pub font_asset_path:
        Option<unsafe extern "system" fn(u32, *mut *const u8, *mut u32) -> i32>,
    /// `svg_has_font_asset`.
    pub has_font_asset: Option<unsafe extern "system" fn(*const u8, u32) -> i32>,
    /// `svg_add_font`.
    pub add_font: Option<unsafe extern "system" fn(*const u8, u32, *const u8, u32) -> i32>,
    /// `svg_parse`.
    pub parse: Option<unsafe extern "system" fn(*const u8, u32, *mut u64) -> i32>,
    /// `svg_dispose`.
    pub dispose: Option<unsafe extern "system" fn(u64) -> i32>,
    /// `svg_render`.
    pub render: Option<
        unsafe extern "system" fn(
            u64,
            u32,
            u32,
            u32,
            f32,
            *mut u8,
            u32,
            *mut u32,
            *mut GpuiGoSvgRenderInfo,
        ) -> i32,
    >,
    /// `svg_render_alpha_mask`.
    pub render_alpha_mask: Option<
        unsafe extern "system" fn(*const u8, u32, u32, u32, *mut u8, u32, *mut u32, *mut GpuiGoSvgRenderInfo) -> i32,
    >,
    /// Service version (bumped on ABI-shape changes of this table).
    pub service_version: u32,
    /// `size_of::<GpuiGoSvgTable>()` — record size self-check.
    pub size_of_table: u32,
    /// `align_of::<GpuiGoSvgTable>()` — record alignment self-check.
    pub align_of_table: u32,
    /// `size_of::<GpuiGoSvgRenderInfo>()` — record size self-check.
    pub size_of_render_info: u32,
    /// The live handle bound ([`MAX_SVG_SLOTS`]).
    pub max_svg_handles: u32,
    /// The accepted byte-length bound ([`MAX_SVG_BYTES`]).
    pub max_svg_bytes: u32,
    /// The pinned smooth-scale factor (for the caller's math).
    pub smooth_scale_factor: f32,
    /// The pinned font-asset path count (the NeedsFontAssets surface).
    pub font_asset_paths: u32,
    /// `svg_mode::SIZE`.
    pub mode_size: u32,
    /// `svg_mode::EXACT_SIZE`.
    pub mode_exact_size: u32,
    /// `svg_mode::SCALE_FACTOR`.
    pub mode_scale_factor: u32,
}

/// The SVG service version.
pub const GPUI_GO_SVG_SERVICE_VERSION: u32 = 1;

pub static SVG_TABLE: GpuiGoSvgTable = GpuiGoSvgTable {
    font_asset_count: Some(svg_font_asset_count),
    font_asset_path: Some(svg_font_asset_path),
    has_font_asset: Some(svg_has_font_asset),
    add_font: Some(svg_add_font),
    parse: Some(svg_parse),
    dispose: Some(svg_dispose),
    render: Some(svg_render),
    render_alpha_mask: Some(svg_render_alpha_mask),
    service_version: GPUI_GO_SVG_SERVICE_VERSION,
    size_of_table: std::mem::size_of::<GpuiGoSvgTable>() as u32,
    align_of_table: std::mem::align_of::<GpuiGoSvgTable>() as u32,
    size_of_render_info: std::mem::size_of::<GpuiGoSvgRenderInfo>() as u32,
    max_svg_handles: MAX_SVG_SLOTS,
    max_svg_bytes: MAX_SVG_BYTES,
    smooth_scale_factor: SVG_SMOOTH_SCALE_FACTOR,
    font_asset_paths: SVG_FONT_ASSET_PATHS.len() as u32,
    mode_size: svg_mode::SIZE,
    mode_exact_size: svg_mode::EXACT_SIZE,
    mode_scale_factor: svg_mode::SCALE_FACTOR,
};

// Compile-time layout pins (the Go loader mirrors these exactly).
const _: () = assert!(std::mem::size_of::<GpuiGoSvgTable>() == 112);
const _: () = assert!(std::mem::align_of::<GpuiGoSvgTable>() == 8);
const _: () = assert!(std::mem::size_of::<GpuiGoSvgRenderInfo>() == 12);
const _: () = assert!(std::mem::align_of::<GpuiGoSvgRenderInfo>() == 4);
