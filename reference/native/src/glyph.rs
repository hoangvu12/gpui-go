//! gpui-go native glyph raster service (ticket10, reserved slot 5).
//!
//! Rasterizes glyphs with the **pinned Windows DirectWrite rasterizer**
//! ported line-by-line from `crates/gpui_windows/src/font_rasterizer.rs`
//! at `254b5dbd47cbb5acbcc5bbdcbb322a339276c88a` (all citations below are
//! `font_rasterizer.rs:<lines>` at that commit), and exposes the pinned
//! raster output surface (`RasterizedGlyph`) through a private C ABI
//! service table.
//!
//! # The port decision the ticket asked to record: face acquisition
//!
//! The pinned `WindowsGlyphRasterizer` implements
//! `gpui_ce_parley::GlyphRasterizer::rasterize(face: RasterFace<'_>,
//! params: &RenderGlyphParams)`. The `RasterFace` (the exact selected
//! font-data bytes, face index, variation coordinates and synthesis —
//! `crates/gpui_ce_parley/src/store.rs:62`) is supplied by the Parley text
//! stack itself: `PlatformTextSystem::rasterize_glyph`
//! (`crates/gpui_ce_parley/src/text_system.rs:1473`) looks the canonical
//! `FontId` up in its Fontique-backed `FontStore` and passes
//! `fonts.get(font_id).raster_face(font_id)` to the injected rasterizer.
//! DirectWrite faces are then created from those exact bytes through the
//! `IDWriteInMemoryFontFileLoader` (`NativeFace::new`,
//! font_rasterizer.rs:652-714), never through a system font enumeration
//! that could select a different face.
//!
//! The service therefore constructs the process-global text stack (the
//! one `crate::text` owns) with this ported rasterizer —
//! `ParleyTextSystem::new_with_rasterizer(SystemFonts::Load, "Segoe UI",
//! WindowsGlyphRasterizer::new())`, the exact pinned Windows platform
//! construction (`crates/gpui_windows/src/platform.rs:117-126`). The
//! rasterize entry calls the **public** `gpui::TextSystem::rasterize_glyph`
//! on that stack, which is exactly the call the pinned window paint path
//! makes (`crates/gpui/src/window.rs::paint_glyph_from_atlas` →
//! `text_system.rasterize_glyph(&params)`), including its `validate()`
//! and metadata-consistency checks. Ticket09's text service recorded the
//! Swash-rasterizer substitution as a temporary deviation ("the
//! glyph-pixels ticket must revisit this"); this ticket is that revisit:
//! the stack now carries the pinned DirectWrite-first rasterizer. The
//! text service's geometry entries are unaffected (no recorded
//! text-geometry output depends on the rasterizer; shaping's `is_emoji`
//! flags are not part of the fx-0004 trace), so the fx-0004 gate stays
//! green while font ids and font metrics stay identical (same store,
//! same interning).
//!
//! # Formats supported (the pin's honest surface)
//!
//! * `AlphaMask` — one coverage byte per pixel. Grayscale mode through
//!   DirectWrite's `DWRITE_TEXTURE_ALIASED_1x1` alpha textures
//!   (`rasterize_mask`, font_rasterizer.rs:305-357).
//! * `BgraSubpixelMask` — four bytes per pixel in blue, green, red,
//!   unused order. Subpixel mode through DirectWrite's ClearType
//!   `DWRITE_TEXTURE_CLEARTYPE_3x1` textures expanded BGRA (the pin
//!   reverses the RGB to BGR, font_rasterizer.rs:359-403), or through the
//!   Swash path's subpixel expansion.
//! * `BgraColor` — four bytes per pixel in blue, green, red,
//!   straight-alpha order. Color mode through:
//!     - DirectWrite COLRv0 layer rasterization with gamma/contrast
//!       corrected coverage (`rasterize_colr`, font_rasterizer.rs:362-537);
//!     - DirectWrite monochrome coverage tinted with the prepared
//!       currentColor (`rasterize_native_monochrome_color`,
//!       font_rasterizer.rs:539-570) for color-mode glyphs with no color
//!       artwork;
//!     - the Swash fallback for the typed unsupported cases below.
//!
//! # Unsupported color behavior (preserved honestly)
//!
//! `supports_color_glyph` reports exactly the pin's predicate
//! (`ColrV0 | Bitmap`, font_rasterizer.rs:87-89). Per-glyph, the
//! DirectWrite path returns typed `NativeRasterUnsupported` errors for
//! **bitmap color glyphs** (CBDT/sbix, font_rasterizer.rs:598-600),
//! **COLRv1 paint graphs** (font_rasterizer.rs:602-604) and **variable
//! axes on legacy DirectWrite** (font_rasterizer.rs:249-253) — and the
//! outer `WindowsGlyphRasterizer` then routes those glyphs to the
//! retained `SwashGlyphRasterizer` fallback (font_rasterizer.rs:99-106),
//! which is why the pinned rasterizer still produces pixels for them.
//! The capability predicate is NOT a claim of COLRv1 support: the pin's
//! doc comment on the enum says exactly that, and this service records
//! the same distinction through the backend-info record. Other raster
//! failures remain errors (`Err`), never silent fallbacks.
//!
//! # Deviations from the pin (each mechanical, none silent)
//!
//! * Visibility: the pinned type is `pub(crate)` inside `gpui_windows`;
//!   this port is a crate-private type inside this artifact. The code is
//!   otherwise ported statement-for-statement.
//! * `log::warn!`/`log::debug!` calls are dropped: the artifact links no
//!   logger, and the pin's behavior is unchanged without one.
//! * The constructor records the DirectWrite construction facts (backend,
//!   gamma, enhanced contrast, system subpixel setting) in a process
//!   global so the ABI's `backend_info` entry can report them; the pin
//!   keeps the same values privately in `ColorRenderingParams`.
//!
//! # ABI rules (same conventions as TEXT_ABI.md)
//!
//! * C ABI, `extern "system"`, `#[repr(C)]`, fixed-width types; f32
//!   values cross as IEEE-754 bits; no Rust strings/Vecs/enums/bools
//!   cross; no Go pointers are retained (input records are copied or
//!   borrowed for the call; output records and the pixel dump buffer are
//!   borrowed for the call only).
//! * Every export contains panics with `catch_unwind`; a contained panic
//!   returns [`glyph_status::ERR_PANIC`] without poisoning any service.
//! * Font ids are the pinned canonical `FontId` values and are validated
//!   against the text stack's registry (unknown ids are typed errors,
//!   never the store's `expect` panic).
//! * The pixel dump uses the capacity protocol: `out_needed` always
//!   carries the required byte count; a too-small capacity copies no
//!   pixel bytes and returns [`glyph_status::ERR_CAPACITY`] while the
//!   metadata record is still written (the raster work is performed
//!   either way; the caller retries with the reported size).

use std::collections::HashMap;
use std::mem::ManuallyDrop;
use std::panic::{catch_unwind, AssertUnwindSafe};
use std::sync::{Arc, OnceLock};

use anyhow::{Context as _, Result, bail, ensure};
use gpui::{
    Bounds, DevicePixels, GlyphRenderMode, GlyphId, PreparedRasterStyle, RasterColorEffect,
    RasterColorExt, RasterStyleRequest, RasterizedGlyph, RasterizedGlyphFormat, RenderGlyphParams,
    Rgba, Rgba8, SUBPIXEL_VARIANTS_X, SUBPIXEL_VARIANTS_Y, TextRenderingMode, px, size,
    FontId, PlatformTextSystem, TextSystem,
};
use gpui_ce_parley::{ColorGlyphKind, GlyphRasterizer, ParleyTextSystem, RasterFace,
    SwashGlyphRasterizer};
use windows::{
    Win32::{
        Foundation::RECT,
        Graphics::DirectWrite::*,
        UI::WindowsAndMessaging::{
            FE_FONTSMOOTHINGCLEARTYPE, SPI_GETFONTSMOOTHING, SPI_GETFONTSMOOTHINGTYPE,
            SYSTEM_PARAMETERS_INFO_UPDATE_FLAGS, SystemParametersInfoW,
        },
    },
    core::{BOOL, Interface},
};
use windows_numerics::Vector2;

use crate::text;

/// Glyph service ABI version (bumped only on record/table layout changes).
pub const GPUI_GO_GLYPH_SERVICE_VERSION: u32 = 1;

/// Maximum accepted raster pixel byte count (the pin has no such bound;
/// the ABI adds one so a caller cannot request unbounded native buffers).
pub const MAX_RASTER_BYTES: u32 = 64 << 20;

/// Maximum accepted font size in pixels (ABI bound; the pin accepts any
/// finite size but its callers pass validated window sizes).
pub const MAX_FONT_SIZE: f32 = 4096.0;

/// Maximum accepted scale factor (ABI bound; the pin only requires
/// finite and > 0).
pub const MAX_SCALE: f32 = 128.0;

/// Glyph service status codes (mirrored in internal/native/glyph.go).
pub mod glyph_status {
    /// Success.
    pub const OK: i32 = 0;
    /// A font id outside the canonical-bit/registry rules.
    pub const ERR_BAD_HANDLE: i32 = -2;
    /// Null argument.
    pub const ERR_NULL_ARG: i32 = -3;
    /// Bad value (mode/effect enumerant, non-finite float, bound, glyph
    /// id overflow, reserved field).
    pub const ERR_BAD_VALUE: i32 = -4;
    /// The pinned rasterization returned an error (font missing from the
    /// store, DirectWrite/Swash failure).
    pub const ERR_RASTER: i32 = -5;
    /// Pixel dump capacity too small (out_needed carries the byte count).
    pub const ERR_CAPACITY: i32 = -7;
    /// A panic was contained by catch_unwind before returning.
    pub const ERR_PANIC: i32 = 101;
}

/// Raster render mode tags (the pinned `GlyphRenderMode` discriminants,
/// `crates/gpui/src/text_system.rs:655`).
pub mod raster_mode {
    /// A color-independent, one-channel coverage mask.
    pub const GRAYSCALE: u32 = 0;
    /// A color-independent, three-channel subpixel coverage mask.
    pub const SUBPIXEL: u32 = 1;
    /// A color glyph whose pixels include their final RGB values.
    pub const COLOR: u32 = 2;
}

/// Raster color effect tags (the pinned `RasterColorEffect` variants,
/// `crates/gpui/src/text_system.rs:729`). Dilation is a CoreGraphics-only
/// effect this backend never produces; the ABI rejects it.
pub mod raster_effect {
    /// Coverage and color pixels do not depend on the scene color.
    pub const INDEPENDENT: u32 = 0;
    /// A quantized color consumed by the DirectWrite preblend/currentColor
    /// path.
    pub const PREBLEND: u32 = 1;
    /// CoreGraphics font-smoothing dilation (unsupported here).
    pub const DILATION: u32 = 2;
}

/// Rasterized format tags (the pinned `RasterizedGlyphFormat` variants,
/// `crates/gpui/src/text_system.rs:752`).
pub mod raster_format {
    /// One byte of coverage per pixel.
    pub const ALPHA_MASK: u32 = 0;
    /// Four bytes per pixel in blue, green, red, unused order.
    pub const BGRA_SUBPIXEL_MASK: u32 = 1;
    /// Four bytes per pixel in blue, green, red, straight-alpha order.
    pub const BGRA_COLOR: u32 = 2;
}

/// Recommended text rendering mode tags (the pinned `TextRenderingMode`
/// variants, `crates/gpui/src/platform.rs:3098`).
pub mod rendering_mode {
    /// Use the platform's default text rendering mode.
    pub const PLATFORM_DEFAULT: u32 = 0;
    /// Use subpixel (ClearType-style) text rendering.
    pub const SUBPIXEL: u32 = 1;
    /// Use grayscale text rendering.
    pub const GRAYSCALE: u32 = 2;
}

/// Backend tags of the backend-info record.
pub mod raster_backend {
    /// DirectWrite with the retained Swash per-glyph fallback (the pinned
    /// `WindowsRasterBackend::DirectWrite`).
    pub const DIRECTWRITE: u32 = 0;
    /// Swash only (DirectWrite initialization failed; the pinned
    /// `WindowsRasterBackend::Swash`).
    pub const SWASH: u32 = 1;
}

// ---------------------------------------------------------------------------
// ABI records
// ---------------------------------------------------------------------------

/// Prepared raster style record (mirrors the pinned `PreparedRasterStyle`:
/// mode plus the color effect). Layout (x86-64, `#[repr(C)]`):
/// `mode` @0, `color_effect_tag` @4, `color_r` @8, `color_g` @12,
/// `color_b` @16, `color_a` @20, `dilation` @24 (must be 0), `reserved`
/// @28 (must be 0), `record_size` @32; size 36, alignment 4.
#[repr(C)]
pub struct GpuiGoRasterStyleRecord {
    /// Render mode tag; see [`raster_mode`].
    pub mode: u32,
    /// Color effect tag; see [`raster_effect`].
    pub color_effect_tag: u32,
    /// Preblend color red byte (0 when the effect is not PREBLEND).
    pub color_r: u32,
    /// Preblend color green byte.
    pub color_g: u32,
    /// Preblend color blue byte.
    pub color_b: u32,
    /// Preblend color alpha byte.
    pub color_a: u32,
    /// Dilation level; must be 0 (the Windows backend never produces it).
    pub dilation: u32,
    /// Reserved; must be 0.
    pub reserved: u32,
    /// Self-check: `size_of::<GpuiGoRasterStyleRecord>()`.
    pub record_size: u32,
}

impl Default for GpuiGoRasterStyleRecord {
    fn default() -> Self {
        Self {
            mode: raster_mode::GRAYSCALE,
            color_effect_tag: raster_effect::INDEPENDENT,
            color_r: 0,
            color_g: 0,
            color_b: 0,
            color_a: 0,
            dilation: 0,
            reserved: 0,
            record_size: core::mem::size_of::<GpuiGoRasterStyleRecord>() as u32,
        }
    }
}

/// Rasterized glyph record (mirrors the pinned `RasterizedGlyph`'s
/// metadata: bounds, buffer size, format, byte count; the pixels
/// themselves are the bulk dump). Layout (x86-64, `#[repr(C)]`):
/// `format` @0, `bounds_x` @4, `bounds_y` @8, `bounds_w` @12,
/// `bounds_h` @16, `width` @20, `height` @24, `pixel_count` @28,
/// `record_size` @32; size 36, alignment 4.
///
/// `bounds` is the pinned placement relative to the glyph's baseline
/// origin (`RasterizedGlyph::bounds`, `crates/gpui/src/text_system.rs:
/// 763-768`): origin `(left, -top)` in device pixels, size equal to the
/// buffer size. The pinned record carries no advance field (the
/// CLARIFIED deviation recorded in TEXT_ABI.md applies here too: pen
/// advances are a shaping concept, observable through the shaping
/// service's glyph positions).
#[repr(C)]
pub struct GpuiGoRasterRecord {
    /// Format tag; see [`raster_format`].
    pub format: u32,
    /// Bounds origin x in device pixels (the pinned left).
    pub bounds_x: i32,
    /// Bounds origin y in device pixels (the pinned -top).
    pub bounds_y: i32,
    /// Bounds width in device pixels.
    pub bounds_w: i32,
    /// Bounds height in device pixels.
    pub bounds_h: i32,
    /// Pixel buffer width (== bounds_w in the pin).
    pub width: i32,
    /// Pixel buffer height (== bounds_h in the pin).
    pub height: i32,
    /// Number of pixel bytes (1 or 4 per pixel per the format).
    pub pixel_count: u32,
    /// Self-check: `size_of::<GpuiGoRasterRecord>()`.
    pub record_size: u32,
}

impl Default for GpuiGoRasterRecord {
    fn default() -> Self {
        Self {
            format: raster_format::ALPHA_MASK,
            bounds_x: 0,
            bounds_y: 0,
            bounds_w: 0,
            bounds_h: 0,
            width: 0,
            height: 0,
            pixel_count: 0,
            record_size: core::mem::size_of::<GpuiGoRasterRecord>() as u32,
        }
    }
}

/// Raster backend info record (the construction facts the pin keeps
/// private: which backend was selected, the DirectWrite gamma and
/// grayscale enhanced contrast, and the OS subpixel setting). Layout:
/// `backend` @0, `variable_factory` @4, `gamma_bits` @8,
/// `contrast_bits` @12, `system_subpixel` @16, `record_size` @20;
/// size 24, alignment 4.
#[repr(C)]
pub struct GpuiGoRasterBackendRecord {
    /// Backend tag; see [`raster_backend`].
    pub backend: u32,
    /// 1 when IDWriteFactory6 was available (variable axes support).
    pub variable_factory: u32,
    /// The DirectWrite rendering gamma (f32 bits; 0 for the Swash
    /// backend).
    pub gamma_bits: u32,
    /// The DirectWrite grayscale enhanced contrast (f32 bits; 0 for the
    /// Swash backend).
    pub contrast_bits: u32,
    /// 1 when the OS font smoothing is enabled with ClearType (the
    /// recommended mode is Subpixel then).
    pub system_subpixel: u32,
    /// Self-check: `size_of::<GpuiGoRasterBackendRecord>()`.
    pub record_size: u32,
}

const _: () = assert!(std::mem::size_of::<GpuiGoRasterStyleRecord>() == 36);
const _: () = assert!(std::mem::align_of::<GpuiGoRasterStyleRecord>() == 4);
const _: () = assert!(std::mem::size_of::<GpuiGoRasterRecord>() == 36);
const _: () = assert!(std::mem::align_of::<GpuiGoRasterRecord>() == 4);
const _: () = assert!(std::mem::size_of::<GpuiGoRasterBackendRecord>() == 24);
const _: () = assert!(std::mem::align_of::<GpuiGoRasterBackendRecord>() == 4);

// ---------------------------------------------------------------------------
// Service table
// ---------------------------------------------------------------------------

/// The glyph raster service table: 6 function pointers then 9 self-check
/// scalars; size 88, alignment 8. All entries return an i32 status.
#[repr(C)]
pub struct GpuiGoGlyphTable {
    /// `glyph_prepare_style`.
    pub prepare_style: Option<
        unsafe extern "system" fn(
            color_r_bits: u32,
            color_g_bits: u32,
            color_b_bits: u32,
            color_a_bits: u32,
            requested_mode: u32,
            out: *mut GpuiGoRasterStyleRecord,
        ) -> i32,
    >,
    /// `glyph_for_char`.
    pub glyph_for_char: Option<
        unsafe extern "system" fn(font_id: u64, char_code: u32, out_glyph_id: *mut u32) -> i32,
    >,
    /// `glyph_rasterize` (metadata record + pixel bulk dump).
    pub rasterize: Option<
        unsafe extern "system" fn(
            font_id: u64,
            glyph_id: u32,
            font_size_bits: u32,
            subpixel_x: u32,
            subpixel_y: u32,
            scale_bits: u32,
            style: *const GpuiGoRasterStyleRecord,
            out: *mut GpuiGoRasterRecord,
            pixels: *mut u8,
            pixel_capacity: u32,
            out_needed: *mut u32,
        ) -> i32,
    >,
    /// `glyph_recommended_mode`.
    pub recommended_mode: Option<unsafe extern "system" fn(out_mode: *mut u32) -> i32>,
    /// `glyph_backend_info`.
    pub backend_info: Option<unsafe extern "system" fn(out: *mut GpuiGoRasterBackendRecord) -> i32>,
    /// `glyph_panic_probe` (test-only; panics inside catch_unwind).
    pub panic_probe: Option<unsafe extern "system" fn() -> i32>,
    /// Glyph ABI version (see [`GPUI_GO_GLYPH_SERVICE_VERSION`]).
    pub service_version: u32,
    /// `size_of::<GpuiGoGlyphTable>()` self-check.
    pub size_of_table: u32,
    /// `align_of::<GpuiGoGlyphTable>()` self-check.
    pub align_of_table: u32,
    /// `size_of::<GpuiGoRasterStyleRecord>()` self-check.
    pub size_of_style_record: u32,
    /// `size_of::<GpuiGoRasterRecord>()` self-check.
    pub size_of_raster_record: u32,
    /// `size_of::<GpuiGoRasterBackendRecord>()` self-check.
    pub size_of_backend_record: u32,
    /// [`SUBPIXEL_VARIANTS_X`] (4).
    pub subpixel_variants_x: u32,
    /// [`SUBPIXEL_VARIANTS_Y`] (1).
    pub subpixel_variants_y: u32,
    /// [`MAX_RASTER_BYTES`].
    pub max_raster_bytes: u32,
}

const _: () = assert!(std::mem::size_of::<GpuiGoGlyphTable>() == 88);
const _: () = assert!(std::mem::align_of::<GpuiGoGlyphTable>() == 8);

/// The one static glyph service table. Its address is stable for the
/// process lifetime.
pub(crate) static GLYPH_TABLE: GpuiGoGlyphTable = GpuiGoGlyphTable {
    prepare_style: Some(glyph_prepare_style),
    glyph_for_char: Some(glyph_for_char),
    rasterize: Some(glyph_rasterize),
    recommended_mode: Some(glyph_recommended_mode),
    backend_info: Some(glyph_backend_info),
    panic_probe: Some(glyph_panic_probe),
    service_version: GPUI_GO_GLYPH_SERVICE_VERSION,
    size_of_table: std::mem::size_of::<GpuiGoGlyphTable>() as u32,
    align_of_table: std::mem::align_of::<GpuiGoGlyphTable>() as u32,
    size_of_style_record: std::mem::size_of::<GpuiGoRasterStyleRecord>() as u32,
    size_of_raster_record: std::mem::size_of::<GpuiGoRasterRecord>() as u32,
    size_of_backend_record: std::mem::size_of::<GpuiGoRasterBackendRecord>() as u32,
    subpixel_variants_x: SUBPIXEL_VARIANTS_X as u32,
    subpixel_variants_y: SUBPIXEL_VARIANTS_Y as u32,
    max_raster_bytes: MAX_RASTER_BYTES,
};

// ---------------------------------------------------------------------------
// The pinned Windows rasterizer (ported from font_rasterizer.rs)
// ---------------------------------------------------------------------------

/// Uses DirectWrite where its required interfaces are available and
/// retains the old OS range through the portable rasterizer otherwise
/// (font_rasterizer.rs:28-42).
pub(crate) struct WindowsGlyphRasterizer {
    backend: WindowsRasterBackend,
    system_subpixel_rendering: bool,
}

enum WindowsRasterBackend {
    DirectWrite {
        rasterizer: DirectWriteGlyphRasterizer,
        fallback: SwashGlyphRasterizer,
    },
    Swash(SwashGlyphRasterizer),
}

/// The typed unsupported cases (font_rasterizer.rs:44-65): each is routed
/// to the Swash fallback by the outer rasterizer, never silently.
#[derive(Debug)]
enum NativeRasterUnsupported {
    VariableAxesOnLegacyDirectWrite,
    BitmapColorGlyph,
    ColrV1Glyph,
}

impl std::fmt::Display for NativeRasterUnsupported {
    fn fmt(&self, formatter: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::VariableAxesOnLegacyDirectWrite => {
                formatter.write_str("this DirectWrite version cannot instantiate variable axes")
            }
            Self::BitmapColorGlyph => formatter
                .write_str("the DirectWrite layer rasterizer does not handle bitmap glyphs"),
            Self::ColrV1Glyph => {
                formatter.write_str("the DirectWrite layer rasterizer does not handle COLRv1")
            }
        }
    }
}

impl std::error::Error for NativeRasterUnsupported {}

/// Construction facts recorded for the ABI's backend-info entry (the
/// values the pin keeps privately in `ColorRenderingParams`, plus the
/// raw DirectWrite gamma they derive from).
#[derive(Clone, Copy)]
pub(crate) struct BackendFacts {
    backend: u32,
    variable_factory: bool,
    gamma: f32,
    grayscale_enhanced_contrast: f32,
    system_subpixel_rendering: bool,
}

static BACKEND_FACTS: OnceLock<BackendFacts> = OnceLock::new();

/// Constructs the pinned rasterizer and records its backend facts.
/// Called by `crate::text::ensure_stack` (the pinned Windows platform
/// construction site).
pub(crate) fn new_windows_rasterizer() -> WindowsGlyphRasterizer {
    let rasterizer = WindowsGlyphRasterizer::new();
    let facts = match &rasterizer.backend {
        WindowsRasterBackend::DirectWrite { rasterizer, .. } => BackendFacts {
            backend: raster_backend::DIRECTWRITE,
            variable_factory: rasterizer.variable_factory.is_some(),
            gamma: unsafe { rasterizer.rendering_params.GetGamma() },
            grayscale_enhanced_contrast: unsafe {
                rasterizer.rendering_params.GetEnhancedContrast()
            },
            system_subpixel_rendering: rasterizer.system_subpixel_rendering,
        },
        WindowsRasterBackend::Swash(_) => BackendFacts {
            backend: raster_backend::SWASH,
            variable_factory: false,
            gamma: 0.0,
            grayscale_enhanced_contrast: 0.0,
            system_subpixel_rendering: rasterizer.system_subpixel_rendering,
        },
    };
    let _ = BACKEND_FACTS.set(facts);
    rasterizer
}

/// The recorded backend facts (defaults before the first stack
/// construction: the Swash-only placeholder the text service documented).
pub(crate) fn backend_facts() -> BackendFacts {
    *BACKEND_FACTS.get_or_init(|| BackendFacts {
        backend: raster_backend::SWASH,
        variable_factory: false,
        gamma: 0.0,
        grayscale_enhanced_contrast: 0.0,
        system_subpixel_rendering: get_system_subpixel_rendering(),
    })
}

impl WindowsGlyphRasterizer {
    /// The pinned constructor (font_rasterizer.rs:67-85): DirectWrite
    /// first; on failure the Swash-only backend (the pin logs a warning;
    /// no logger is linked here).
    pub(crate) fn new() -> Self {
        let backend = match DirectWriteGlyphRasterizer::new() {
            Ok(rasterizer) => WindowsRasterBackend::DirectWrite {
                rasterizer,
                fallback: SwashGlyphRasterizer::default(),
            },
            Err(_error) => WindowsRasterBackend::Swash(SwashGlyphRasterizer::default()),
        };

        Self {
            backend,
            system_subpixel_rendering: get_system_subpixel_rendering(),
        }
    }
}

impl GlyphRasterizer for WindowsGlyphRasterizer {
    /// The pinned capability predicate (font_rasterizer.rs:87-89).
    fn supports_color_glyph(&self, kind: ColorGlyphKind) -> bool {
        matches!(kind, ColorGlyphKind::ColrV0 | ColorGlyphKind::Bitmap)
    }

    fn prepare_style(&self, request: RasterStyleRequest) -> PreparedRasterStyle {
        match &self.backend {
            WindowsRasterBackend::DirectWrite { rasterizer, .. } => {
                rasterizer.prepare_style(request)
            }
            WindowsRasterBackend::Swash(rasterizer) => rasterizer.prepare_style(request),
        }
    }

    /// The pinned per-glyph fallback routing (font_rasterizer.rs:98-113):
    /// typed unsupported cases go to the retained Swash rasterizer; other
    /// errors stay errors.
    fn rasterize(
        &mut self,
        face: RasterFace<'_>,
        params: &RenderGlyphParams,
    ) -> Result<RasterizedGlyph> {
        match &mut self.backend {
            WindowsRasterBackend::DirectWrite {
                rasterizer,
                fallback,
            } => match rasterizer.rasterize(face, params) {
                Ok(glyph) => Ok(glyph),
                Err(error) if error.downcast_ref::<NativeRasterUnsupported>().is_some() => {
                    fallback.rasterize(face, params)
                }
                Err(error) => Err(error),
            },
            WindowsRasterBackend::Swash(rasterizer) => rasterizer.rasterize(face, params),
        }
    }

    fn recommended_mode(&self) -> TextRenderingMode {
        if self.system_subpixel_rendering {
            TextRenderingMode::Subpixel
        } else {
            TextRenderingMode::Grayscale
        }
    }
}

/// DirectWrite rasterization for the exact face and instance selected by
/// Parley (font_rasterizer.rs:130-157).
pub(crate) struct DirectWriteGlyphRasterizer {
    factory: IDWriteFactory5,
    variable_factory: Option<IDWriteFactory6>,
    in_memory_loader: IDWriteInMemoryFontFileLoader,
    rendering_params: IDWriteRenderingParams,
    faces: HashMap<gpui::FontId, NativeFace>,
    color_rendering: ColorRenderingParams,
    system_subpixel_rendering: bool,
}

struct NativeFace {
    face: IDWriteFontFace3,
    _data: Box<[u8]>,
}

struct GlyphAnalysis {
    analysis: IDWriteGlyphRunAnalysis,
    bounds: RECT,
    texture_type: DWRITE_TEXTURE_TYPE,
}

struct ColorRenderingParams {
    gamma_ratios: [f32; 4],
    grayscale_enhanced_contrast: f32,
}

#[derive(Clone, Copy)]
struct LayerColor {
    red: f32,
    green: f32,
    blue: f32,
    alpha: f32,
}

impl DirectWriteGlyphRasterizer {
    /// The pinned constructor (font_rasterizer.rs:167-197): factory,
    /// variable-factory cast, the in-memory font loader (registered for
    /// the process), the rendering parameters and the gamma/contrast
    /// correction facts.
    pub(crate) fn new() -> Result<Self> {
        let factory: IDWriteFactory5 = unsafe { DWriteCreateFactory(DWRITE_FACTORY_TYPE_SHARED) }
            .context("creating the DirectWrite factory")?;
        let variable_factory = factory.cast().ok();
        let in_memory_loader = unsafe { factory.CreateInMemoryFontFileLoader() }
            .context("creating the DirectWrite in-memory font loader")?;
        unsafe { factory.RegisterFontFileLoader(&in_memory_loader) }
            .context("registering the DirectWrite in-memory font loader")?;
        let rendering_params = unsafe { factory.CreateRenderingParams() }
            .context("reading DirectWrite rendering parameters")?;
        let grayscale_rendering_params: IDWriteRenderingParams1 = rendering_params
            .cast()
            .context("reading DirectWrite grayscale rendering parameters")?;
        let color_rendering = ColorRenderingParams {
            gamma_ratios: gpui::get_gamma_correction_ratios(unsafe {
                grayscale_rendering_params.GetGamma()
            }),
            grayscale_enhanced_contrast: unsafe {
                grayscale_rendering_params.GetGrayscaleEnhancedContrast()
            },
        };

        Ok(Self {
            factory,
            variable_factory,
            in_memory_loader,
            rendering_params,
            faces: HashMap::default(),
            color_rendering,
            system_subpixel_rendering: get_system_subpixel_rendering(),
        })
    }

    /// The pinned face cache (font_rasterizer.rs:200-224): one DirectWrite
    /// face per canonical FontId, created from the exact RasterFace bytes.
    fn native_face(&mut self, face: &RasterFace<'_>) -> Result<IDWriteFontFace3> {
        if !face.variations.is_empty() && self.variable_factory.is_none() {
            return Err(NativeRasterUnsupported::VariableAxesOnLegacyDirectWrite.into());
        }

        match self.faces.entry(face.font_id) {
            std::collections::hash_map::Entry::Occupied(entry) => Ok(entry.get().face.clone()),
            std::collections::hash_map::Entry::Vacant(entry) => {
                let native = NativeFace::new(
                    &self.factory,
                    self.variable_factory.as_ref(),
                    &self.in_memory_loader,
                    face,
                )
                .with_context(|| {
                    format!(
                        "DirectWrite could not create FontId {:?}, face index {}, variations {:?}",
                        face.font_id, face.face_index, face.variations
                    )
                })?;

                Ok(entry.insert(native).face.clone())
            }
        }
    }

    /// The pinned glyph-run analysis (font_rasterizer.rs:226-303): one
    /// glyph, the raster transform from the scale factor, the baseline
    /// origin from the subpixel variant, the recommended rendering mode
    /// (outline promoted to natural symmetric) and the texture type from
    /// the render mode.
    fn create_glyph_analysis(
        &self,
        font_face: &IDWriteFontFace3,
        params: &RenderGlyphParams,
        mode: GlyphRenderMode,
    ) -> Result<GlyphAnalysis> {
        let glyph_id =
            [u16::try_from(params.glyph_id.0).context("DirectWrite glyph IDs are 16-bit")?];
        let advances = [0.0];
        let offsets = [DWRITE_GLYPH_OFFSET::default()];
        let base_face: IDWriteFontFace = font_face.cast()?;
        let glyph_run = DWRITE_GLYPH_RUN {
            fontFace: ManuallyDrop::new(Some(unsafe { std::ptr::read(&base_face) })),
            fontEmSize: f32::from(params.font_size),
            glyphCount: 1,
            glyphIndices: glyph_id.as_ptr(),
            glyphAdvances: advances.as_ptr(),
            glyphOffsets: offsets.as_ptr(),
            isSideways: BOOL(0),
            bidiLevel: 0,
        };

        let transform = raster_transform(params.scale_factor);
        let baseline = baseline_origin(params);
        let mut rendering_mode = DWRITE_RENDERING_MODE1::default();
        let mut grid_fit_mode = DWRITE_GRID_FIT_MODE::default();
        unsafe {
            font_face.GetRecommendedRenderingMode(
                f32::from(params.font_size),
                96.0,
                96.0,
                Some(&transform),
                false,
                DWRITE_OUTLINE_THRESHOLD_ANTIALIASED,
                DWRITE_MEASURING_MODE_NATURAL,
                &self.rendering_params,
                &mut rendering_mode,
                &mut grid_fit_mode,
            )?;
        }

        if rendering_mode == DWRITE_RENDERING_MODE1_OUTLINE {
            rendering_mode = DWRITE_RENDERING_MODE1_NATURAL_SYMMETRIC;
        }

        let (antialias_mode, texture_type) = if mode == GlyphRenderMode::Subpixel {
            (
                DWRITE_TEXT_ANTIALIAS_MODE_CLEARTYPE,
                DWRITE_TEXTURE_CLEARTYPE_3x1,
            )
        } else {
            (
                DWRITE_TEXT_ANTIALIAS_MODE_GRAYSCALE,
                DWRITE_TEXTURE_ALIASED_1x1,
            )
        };

        let analysis = unsafe {
            self.factory.CreateGlyphRunAnalysis(
                &glyph_run,
                Some(&transform),
                rendering_mode,
                DWRITE_MEASURING_MODE_NATURAL,
                grid_fit_mode,
                antialias_mode,
                baseline.X,
                baseline.Y,
            )
        }?;

        let bounds = unsafe { analysis.GetAlphaTextureBounds(texture_type) }?;

        Ok(GlyphAnalysis {
            analysis,
            bounds,
            texture_type,
        })
    }

    /// The pinned mask rasterization (font_rasterizer.rs:305-357):
    /// grayscale mode reads the 1x1 alpha texture; subpixel mode reads
    /// the 3x1 ClearType texture and expands it to BGRA with the pin's
    /// reversed RGB order.
    fn rasterize_mask(
        &self,
        font_face: &IDWriteFontFace3,
        params: &RenderGlyphParams,
        mode: GlyphRenderMode,
    ) -> Result<RasterizedGlyph> {
        let glyph = self.create_glyph_analysis(font_face, params, mode)?;
        let Some((bounds, width, height)) = convert_bounds(glyph.bounds)? else {
            return Ok(RasterizedGlyph::empty(mode.rasterized_format()));
        };

        let pixel_count = width as usize * height as usize;

        if mode != GlyphRenderMode::Subpixel {
            let mut pixels = vec![0; pixel_count];
            unsafe {
                glyph.analysis.CreateAlphaTexture(
                    DWRITE_TEXTURE_ALIASED_1x1,
                    &glyph.bounds,
                    &mut pixels,
                )?;
            }

            return Ok(RasterizedGlyph {
                bounds,
                size: size(DevicePixels(width), DevicePixels(height)),
                format: RasterizedGlyphFormat::AlphaMask,
                pixels,
            });
        }

        let mut pixels = vec![0; pixel_count * 4];
        unsafe {
            glyph.analysis.CreateAlphaTexture(
                glyph.texture_type,
                &glyph.bounds,
                &mut pixels[..pixel_count * 3],
            )?;
        }

        for pixel_index in (0..pixel_count).rev() {
            let source = pixel_index * 3;
            let target = pixel_index * 4;
            let red = pixels[source];
            let green = pixels[source + 1];
            let blue = pixels[source + 2];
            pixels[target..target + 4].copy_from_slice(&[blue, green, red, 0]);
        }

        Ok(RasterizedGlyph {
            bounds,
            size: size(DevicePixels(width), DevicePixels(height)),
            format: RasterizedGlyphFormat::BgraSubpixelMask,
            pixels,
        })
    }

    /// The pinned COLRv0 rasterization (font_rasterizer.rs:362-537): two
    /// passes over the translated color glyph runs — the first unions the
    /// layer bounds, the second composites gamma/contrast-corrected
    /// coverage per layer, straightened to u8 BGRA.
    fn rasterize_colr(
        &self,
        font_face: &IDWriteFontFace3,
        params: &RenderGlyphParams,
    ) -> Result<RasterizedGlyph> {
        let current_color = prepared_color(params.raster_style)?;
        let glyph_id = [u16::try_from(params.glyph_id.0)?];
        let advances = [0.0];
        let offsets = [DWRITE_GLYPH_OFFSET::default()];
        let base_face: IDWriteFontFace = font_face.cast()?;
        let glyph_run = DWRITE_GLYPH_RUN {
            fontFace: ManuallyDrop::new(Some(unsafe { std::ptr::read(&base_face) })),
            fontEmSize: f32::from(params.font_size),
            glyphCount: 1,
            glyphIndices: glyph_id.as_ptr(),
            glyphAdvances: advances.as_ptr(),
            glyphOffsets: offsets.as_ptr(),
            isSideways: BOOL(0),
            bidiLevel: 0,
        };

        let transform = raster_transform(params.scale_factor);
        let baseline = baseline_origin(params);
        let enumerate = || unsafe {
            self.factory.TranslateColorGlyphRun(
                baseline,
                &glyph_run,
                None,
                DWRITE_GLYPH_IMAGE_FORMATS_COLR,
                DWRITE_MEASURING_MODE_NATURAL,
                Some(&transform),
                0,
            )
        };

        let enumerator = enumerate()?;
        let mut raster_bounds: Option<RECT> = None;
        while unsafe { enumerator.MoveNext() }?.as_bool() {
            let run = unsafe { &*enumerator.GetCurrentRun()? };

            if run.glyphImageFormat & DWRITE_GLYPH_IMAGE_FORMATS_COLR
                == DWRITE_GLYPH_IMAGE_FORMATS_NONE
            {
                continue;
            }

            let analysis = unsafe {
                self.factory.CreateGlyphRunAnalysis(
                    &run.Base.glyphRun,
                    Some(&transform),
                    DWRITE_RENDERING_MODE1_NATURAL_SYMMETRIC,
                    run.measuringMode,
                    DWRITE_GRID_FIT_MODE_DEFAULT,
                    DWRITE_TEXT_ANTIALIAS_MODE_GRAYSCALE,
                    run.Base.baselineOriginX,
                    run.Base.baselineOriginY,
                )
            }?;

            let layer_bounds =
                unsafe { analysis.GetAlphaTextureBounds(DWRITE_TEXTURE_ALIASED_1x1) }?;

            if convert_bounds(layer_bounds)?.is_none() {
                continue;
            }

            raster_bounds = Some(match raster_bounds {
                Some(bounds) => RECT {
                    left: bounds.left.min(layer_bounds.left),
                    top: bounds.top.min(layer_bounds.top),
                    right: bounds.right.max(layer_bounds.right),
                    bottom: bounds.bottom.max(layer_bounds.bottom),
                },
                None => layer_bounds,
            });
        }

        let Some(raster_bounds) = raster_bounds else {
            return Ok(RasterizedGlyph::empty(RasterizedGlyphFormat::BgraColor));
        };

        let Some((bounds, width, height)) = convert_bounds(raster_bounds)? else {
            unreachable!("color layer bounds were validated above");
        };

        let mut premultiplied = vec![[0.0f32; 4]; width as usize * height as usize];
        let enumerator = enumerate()?;
        while unsafe { enumerator.MoveNext() }?.as_bool() {
            let run = unsafe { &*enumerator.GetCurrentRun()? };

            if run.glyphImageFormat & DWRITE_GLYPH_IMAGE_FORMATS_COLR
                == DWRITE_GLYPH_IMAGE_FORMATS_NONE
            {
                continue;
            }

            let layer_analysis = unsafe {
                self.factory.CreateGlyphRunAnalysis(
                    &run.Base.glyphRun,
                    Some(&transform),
                    DWRITE_RENDERING_MODE1_NATURAL_SYMMETRIC,
                    run.measuringMode,
                    DWRITE_GRID_FIT_MODE_DEFAULT,
                    DWRITE_TEXT_ANTIALIAS_MODE_GRAYSCALE,
                    run.Base.baselineOriginX,
                    run.Base.baselineOriginY,
                )
            }?;

            let layer_bounds =
                unsafe { layer_analysis.GetAlphaTextureBounds(DWRITE_TEXTURE_ALIASED_1x1) }?;

            let Some((_, layer_width, layer_height)) = convert_bounds(layer_bounds)? else {
                continue;
            };

            let mut coverage = vec![0; layer_width as usize * layer_height as usize];
            unsafe {
                layer_analysis.CreateAlphaTexture(
                    DWRITE_TEXTURE_ALIASED_1x1,
                    &layer_bounds,
                    &mut coverage,
                )?;
            }

            let color = layer_color(run, current_color);
            for layer_y in 0..layer_height {
                let target_y = layer_bounds.top - raster_bounds.top + layer_y;

                if !(0..height).contains(&target_y) {
                    continue;
                }

                for layer_x in 0..layer_width {
                    let target_x = layer_bounds.left - raster_bounds.left + layer_x;

                    if !(0..width).contains(&target_x) {
                        continue;
                    }

                    let source_index = (layer_y as usize * layer_width as usize) + layer_x as usize;
                    let target_index = target_y as usize * width as usize + target_x as usize;
                    let corrected = corrected_coverage(
                        f32::from(coverage[source_index]) / 255.0,
                        color,
                        &self.color_rendering,
                    );
                    composite_color(&mut premultiplied[target_index], color, corrected);
                }
            }
        }

        let mut pixels = Vec::with_capacity(premultiplied.len() * 4);
        for pixel in premultiplied {
            let alpha = pixel[3].clamp(0.0, 1.0);

            if alpha == 0.0 {
                pixels.extend_from_slice(&[0, 0, 0, 0]);
                continue;
            }

            pixels.extend_from_slice(&[
                float_channel(pixel[2] / alpha),
                float_channel(pixel[1] / alpha),
                float_channel(pixel[0] / alpha),
                float_channel(alpha),
            ]);
        }

        Ok(RasterizedGlyph {
            bounds,
            size: size(DevicePixels(width), DevicePixels(height)),
            format: RasterizedGlyphFormat::BgraColor,
            pixels,
        })
    }

    /// The pinned monochrome currentColor rasterization
    /// (font_rasterizer.rs:539-570): grayscale coverage multiplied by the
    /// prepared color's alpha, straight alpha.
    fn rasterize_native_monochrome_color(
        &self,
        glyph: GlyphAnalysis,
        bounds: Bounds<DevicePixels>,
        width: i32,
        height: i32,
        color: Rgba8,
    ) -> Result<RasterizedGlyph> {
        let pixel_count = width as usize * height as usize;
        let mut coverage = vec![0; pixel_count];
        unsafe {
            glyph.analysis.CreateAlphaTexture(
                DWRITE_TEXTURE_ALIASED_1x1,
                &glyph.bounds,
                &mut coverage,
            )?;
        }

        let mut pixels = Vec::with_capacity(pixel_count * 4);
        for alpha in coverage {
            let alpha = multiply_u8(alpha, color.alpha);
            pixels.extend_from_slice(&[color.blue, color.green, color.red, alpha]);
        }

        Ok(RasterizedGlyph {
            bounds,
            size: size(DevicePixels(width), DevicePixels(height)),
            format: RasterizedGlyphFormat::BgraColor,
            pixels,
        })
    }
}

impl Drop for DirectWriteGlyphRasterizer {
    /// The pinned teardown (font_rasterizer.rs:572-580).
    fn drop(&mut self) {
        self.faces.clear();
        unsafe {
            let _ = self
                .factory
                .UnregisterFontFileLoader(&self.in_memory_loader);
        }
    }
}

impl GlyphRasterizer for DirectWriteGlyphRasterizer {
    /// The pinned capability predicate (font_rasterizer.rs:583-585):
    /// COLRv0 only — the typed fallback enum must not be read as COLRv1
    /// support.
    fn supports_color_glyph(&self, kind: ColorGlyphKind) -> bool {
        kind == ColorGlyphKind::ColrV0
    }

    /// The pinned style preparation (font_rasterizer.rs:587-599): color
    /// mode quantizes the scene color into a Preblend effect; other modes
    /// stay color-independent.
    fn prepare_style(&self, request: RasterStyleRequest) -> PreparedRasterStyle {
        if request.requested_mode == GlyphRenderMode::Color {
            PreparedRasterStyle {
                mode: GlyphRenderMode::Color,
                color_effect: RasterColorEffect::Preblend(
                    request.scene_color.quantize_raster_color(),
                ),
            }
        } else {
            PreparedRasterStyle::independent(request.requested_mode)
        }
    }

    /// The pinned rasterization dispatch (font_rasterizer.rs:601-648):
    /// scale validation, the color-glyph classification, the typed
    /// unsupported errors, and the four output paths.
    fn rasterize(
        &mut self,
        face: RasterFace<'_>,
        params: &RenderGlyphParams,
    ) -> Result<RasterizedGlyph> {
        ensure!(
            params.scale_factor.is_finite() && params.scale_factor > 0.0,
            "invalid raster scale factor"
        );
        let color_kind = if params.raster_style.mode == GlyphRenderMode::Color {
            face.color_glyph_kind(params.glyph_id)?
        } else {
            None
        };

        if color_kind == Some(ColorGlyphKind::Bitmap) {
            return Err(NativeRasterUnsupported::BitmapColorGlyph.into());
        }

        if color_kind == Some(ColorGlyphKind::ColrV1) {
            return Err(NativeRasterUnsupported::ColrV1Glyph.into());
        }

        let font_face = self.native_face(&face)?;
        match color_kind {
            Some(ColorGlyphKind::ColrV0) => self.rasterize_colr(&font_face, params),
            Some(ColorGlyphKind::Svg) | None
                if params.raster_style.mode == GlyphRenderMode::Color =>
            {
                let color = prepared_color(params.raster_style)?;
                let glyph =
                    self.create_glyph_analysis(&font_face, params, GlyphRenderMode::Grayscale)?;
                let Some((bounds, width, height)) = convert_bounds(glyph.bounds)? else {
                    return Ok(RasterizedGlyph::empty(RasterizedGlyphFormat::BgraColor));
                };

                self.rasterize_native_monochrome_color(glyph, bounds, width, height, color)
            }
            _ => self.rasterize_mask(&font_face, params, params.raster_style.mode),
        }
    }

    fn recommended_mode(&self) -> TextRenderingMode {
        if self.system_subpixel_rendering {
            TextRenderingMode::Subpixel
        } else {
            TextRenderingMode::Grayscale
        }
    }
}

impl NativeFace {
    /// The pinned face creation (font_rasterizer.rs:652-714): the exact
    /// RasterFace bytes through the in-memory loader, the Fontique
    /// synthesis flags as DWrite simulations, and the variable-axes path
    /// through IDWriteFactory6.
    fn new(
        factory: &IDWriteFactory5,
        variable_factory: Option<&IDWriteFactory6>,
        loader: &IDWriteInMemoryFontFileLoader,
        face: &RasterFace<'_>,
    ) -> Result<Self> {
        let data: Box<[u8]> = face.data.into();
        let data_len = u32::try_from(data.len()).context("font data exceeds DirectWrite limits")?;
        let file = unsafe {
            loader.CreateInMemoryFontFileReference(
                factory,
                data.as_ptr().cast(),
                data_len,
                None::<&windows::core::IUnknown>,
            )
        }?;

        let mut simulations = DWRITE_FONT_SIMULATIONS_NONE;

        if face.synthesis.embolden {
            simulations |= DWRITE_FONT_SIMULATIONS_BOLD;
        }

        if face.synthesis.skew_degrees.is_some() {
            simulations |= DWRITE_FONT_SIMULATIONS_OBLIQUE;
        }

        let native_face = if face.variations.is_empty() {
            let reference =
                unsafe { factory.CreateFontFaceReference(&file, face.face_index, simulations) }?;

            unsafe { reference.CreateFontFace() }?
        } else {
            let variable_factory =
                variable_factory.ok_or(NativeRasterUnsupported::VariableAxesOnLegacyDirectWrite)?;
            let variations = face
                .variations
                .iter()
                .map(|variation| DWRITE_FONT_AXIS_VALUE {
                    axisTag: DWRITE_FONT_AXIS_TAG(u32::from_le_bytes(variation.tag.to_be_bytes())),
                    value: variation.value,
                })
                .collect::<Vec<_>>();
            let reference = unsafe {
                variable_factory.CreateFontFaceReference(
                    &file,
                    face.face_index,
                    simulations,
                    &variations,
                )
            }?;

            let variable_face = unsafe { reference.CreateFontFace() }?;

            variable_face.cast()?
        };

        Ok(Self {
            face: native_face,
            _data: data,
        })
    }
}

/// The pinned bounds conversion (font_rasterizer.rs:717-738): empty
/// textures (right <= left or bottom <= top) become `None`.
fn convert_bounds(bounds: RECT) -> Result<Option<(Bounds<DevicePixels>, i32, i32)>> {
    if bounds.right <= bounds.left || bounds.bottom <= bounds.top {
        return Ok(None);
    }

    let width = bounds
        .right
        .checked_sub(bounds.left)
        .context("DirectWrite glyph width overflow")?;
    let height = bounds
        .bottom
        .checked_sub(bounds.top)
        .context("DirectWrite glyph height overflow")?;
    Ok(Some((
        Bounds {
            origin: gpui::point(DevicePixels(bounds.left), DevicePixels(bounds.top)),
            size: size(DevicePixels(width), DevicePixels(height)),
        },
        width,
        height,
    )))
}

/// The pinned raster transform (font_rasterizer.rs:740-749).
fn raster_transform(scale_factor: f32) -> DWRITE_MATRIX {
    DWRITE_MATRIX {
        m11: scale_factor,
        m12: 0.0,
        m21: 0.0,
        m22: scale_factor,
        dx: 0.0,
        dy: 0.0,
    }
}

/// The pinned baseline origin (font_rasterizer.rs:751-756): the subpixel
/// variant divided by the variant count and the scale factor, carried as
/// the pin's `windows_numerics::Vector2` (both `.X`/`.Y` feed
/// `CreateGlyphRunAnalysis` and the whole vector feeds
/// `TranslateColorGlyphRun`).
fn baseline_origin(params: &RenderGlyphParams) -> Vector2 {
    Vector2::new(
        f32::from(params.subpixel_variant.x) / SUBPIXEL_VARIANTS_X as f32 / params.scale_factor,
        f32::from(params.subpixel_variant.y) / SUBPIXEL_VARIANTS_Y as f32 / params.scale_factor,
    )
}

/// The pinned prepared-color extraction (font_rasterizer.rs:758-763).
fn prepared_color(style: PreparedRasterStyle) -> Result<Rgba8> {
    match style.color_effect {
        RasterColorEffect::Preblend(color) => Ok(color),
        _ => bail!("color glyph rasterization requires a prepared currentColor value"),
    }
}

/// The pinned layer color (font_rasterizer.rs:765-782): the palette color
/// or the prepared currentColor for `DWRITE_NO_PALETTE_INDEX`.
fn layer_color(run: &DWRITE_COLOR_GLYPH_RUN1, current_color: Rgba8) -> LayerColor {
    if u32::from(run.Base.paletteIndex) == DWRITE_NO_PALETTE_INDEX {
        LayerColor {
            red: f32::from(current_color.red) / 255.0,
            green: f32::from(current_color.green) / 255.0,
            blue: f32::from(current_color.blue) / 255.0,
            alpha: f32::from(current_color.alpha) / 255.0,
        }
    } else {
        let color = run.Base.runColor;
        LayerColor {
            red: color.r,
            green: color.g,
            blue: color.b,
            alpha: color.a,
        }
    }
}

/// The pinned gamma/contrast-corrected coverage
/// (font_rasterizer.rs:784-793).
fn corrected_coverage(sample: f32, color: LayerColor, rendering: &ColorRenderingParams) -> f32 {
    let brightness = 0.30 * color.red + 0.59 * color.green + 0.11 * color.blue;
    let light_on_dark = (4.0 * (0.75 - brightness)).clamp(0.0, 1.0);
    let contrast = rendering.grayscale_enhanced_contrast * light_on_dark;
    let contrasted = sample * (contrast + 1.0) / (sample * contrast + 1.0);
    let ratios = rendering.gamma_ratios;
    let brightness_adjustment = ratios[0] * brightness + ratios[1];
    let correction = brightness_adjustment * contrasted + ratios[2] * brightness + ratios[3];
    (contrasted + contrasted * (1.0 - contrasted) * correction).clamp(0.0, 1.0)
}

/// The pinned premultiplied color composite
/// (font_rasterizer.rs:795-802).
fn composite_color(destination: &mut [f32; 4], color: LayerColor, coverage: f32) {
    let source_alpha = (coverage * color.alpha).clamp(0.0, 1.0);
    let inverse_alpha = 1.0 - source_alpha;
    destination[0] = color.red * source_alpha + destination[0] * inverse_alpha;
    destination[1] = color.green * source_alpha + destination[1] * inverse_alpha;
    destination[2] = color.blue * source_alpha + destination[2] * inverse_alpha;
    destination[3] = source_alpha + destination[3] * inverse_alpha;
}

/// The pinned channel conversion (font_rasterizer.rs:804-806).
fn float_channel(value: f32) -> u8 {
    (value * 255.0).round().clamp(0.0, 255.0) as u8
}

/// The pinned byte multiply (font_rasterizer.rs:808-810).
fn multiply_u8(left: u8, right: u8) -> u8 {
    ((u16::from(left) * u16::from(right) + 127) / 255) as u8
}

/// The pinned OS subpixel probe (font_rasterizer.rs:812-837):
/// SPI_GETFONTSMOOTHING plus SPI_GETFONTSMOOTHINGTYPE == ClearType.
fn get_system_subpixel_rendering() -> bool {
    let mut smoothing_enabled = BOOL::default();
    let enabled_result = unsafe {
        SystemParametersInfoW(
            SPI_GETFONTSMOOTHING,
            0,
            Some((&mut smoothing_enabled as *mut BOOL).cast::<core::ffi::c_void>()),
            SYSTEM_PARAMETERS_INFO_UPDATE_FLAGS::default(),
        )
    };

    let mut smoothing_type = std::ffi::c_uint::default();
    let type_result = unsafe {
        SystemParametersInfoW(
            SPI_GETFONTSMOOTHINGTYPE,
            0,
            Some((&mut smoothing_type as *mut std::ffi::c_uint).cast::<core::ffi::c_void>()),
            SYSTEM_PARAMETERS_INFO_UPDATE_FLAGS::default(),
        )
    };

    enabled_result.is_ok()
        && type_result.is_ok()
        && smoothing_enabled.as_bool()
        && smoothing_type == FE_FONTSMOOTHINGCLEARTYPE
}

// ---------------------------------------------------------------------------
// Entry bodies
// ---------------------------------------------------------------------------

/// The process-global stack borrow the raster entries need (the pinned
/// window's TextSystem call site). Locks the text service's mutex for the
/// call; no lock is held across any callback.
fn with_text_system<T>(f: impl FnOnce(&Arc<TextSystem>) -> Result<T, i32>) -> Result<T, i32> {
    text::with_stack(|stack| f(&stack.system))
}

/// Validates and decodes a style record into the pinned
/// `PreparedRasterStyle`. A DILATION tag is rejected: the Windows backend
/// never produces or consumes it (the Swash path bails on it,
/// store.rs:571).
fn style_from_record(record: &GpuiGoRasterStyleRecord) -> Result<PreparedRasterStyle, i32> {
    if record.record_size != core::mem::size_of::<GpuiGoRasterStyleRecord>() as u32 {
        return Err(glyph_status::ERR_BAD_VALUE);
    }
    if record.dilation != 0 || record.reserved != 0 {
        return Err(glyph_status::ERR_BAD_VALUE);
    }
    let mode = match record.mode {
        raster_mode::GRAYSCALE => GlyphRenderMode::Grayscale,
        raster_mode::SUBPIXEL => GlyphRenderMode::Subpixel,
        raster_mode::COLOR => GlyphRenderMode::Color,
        _ => return Err(glyph_status::ERR_BAD_VALUE),
    };
    let effect = match record.color_effect_tag {
        raster_effect::INDEPENDENT => RasterColorEffect::Independent,
        raster_effect::PREBLEND => {
            if record.color_r > 255 || record.color_g > 255 || record.color_b > 255 || record.color_a > 255 {
                return Err(glyph_status::ERR_BAD_VALUE);
            }
            RasterColorEffect::Preblend(Rgba8::new(
                record.color_r as u8,
                record.color_g as u8,
                record.color_b as u8,
                record.color_a as u8,
            ))
        }
        // CoreFonts dilation cannot be requested from this backend.
        _ => return Err(glyph_status::ERR_BAD_VALUE),
    };
    Ok(PreparedRasterStyle { mode, color_effect: effect })
}

/// Encodes a pinned `PreparedRasterStyle` into the ABI record.
fn style_record_of(style: &PreparedRasterStyle) -> GpuiGoRasterStyleRecord {
    let mut rec = GpuiGoRasterStyleRecord::default();
    rec.mode = match style.mode {
        GlyphRenderMode::Grayscale => raster_mode::GRAYSCALE,
        GlyphRenderMode::Subpixel => raster_mode::SUBPIXEL,
        GlyphRenderMode::Color => raster_mode::COLOR,
    };
    match style.color_effect {
        RasterColorEffect::Independent => {
            rec.color_effect_tag = raster_effect::INDEPENDENT;
        }
        RasterColorEffect::Preblend(color) => {
            rec.color_effect_tag = raster_effect::PREBLEND;
            let bytes: [u8; 4] = color.into();
            rec.color_r = u32::from(bytes[0]);
            rec.color_g = u32::from(bytes[1]);
            rec.color_b = u32::from(bytes[2]);
            rec.color_a = u32::from(bytes[3]);
        }
        RasterColorEffect::Dilation(_) => {
            // Unreachable from the Windows backend; encoded as the
            // independent effect with the dilation flag would be a lie,
            // so the record keeps the tag at PREBLEND-incompatible 0.
            rec.color_effect_tag = raster_effect::INDEPENDENT;
        }
    }
    rec
}

/// The pinned Rgba from four f32 bits (the RasterStyleRequest scene color
/// channels; the pin's window converts its Hsla scene color to this Rgba
/// through palette's IntoColor before the request is built).
fn rgba_from_bits(r: u32, g: u32, b: u32, a: u32) -> Result<Rgba, i32> {
    let (r, g, b, a) = (
        f32::from_bits(r),
        f32::from_bits(g),
        f32::from_bits(b),
        f32::from_bits(a),
    );
    if !r.is_finite() || !g.is_finite() || !b.is_finite() || !a.is_finite() {
        return Err(glyph_status::ERR_BAD_VALUE);
    }
    Ok(Rgba::new(r, g, b, a))
}

unsafe fn glyph_prepare_style_body(
    color_r_bits: u32,
    color_g_bits: u32,
    color_b_bits: u32,
    color_a_bits: u32,
    requested_mode: u32,
    out: *mut GpuiGoRasterStyleRecord,
) -> i32 {
    unsafe {
        if out.is_null() {
            return glyph_status::ERR_NULL_ARG;
        }
        let out = &mut *out;
        *out = GpuiGoRasterStyleRecord::default();

        let mode = match requested_mode {
            raster_mode::GRAYSCALE => GlyphRenderMode::Grayscale,
            raster_mode::SUBPIXEL => GlyphRenderMode::Subpixel,
            raster_mode::COLOR => GlyphRenderMode::Color,
            _ => return glyph_status::ERR_BAD_VALUE,
        };
        let scene_color = match rgba_from_bits(color_r_bits, color_g_bits, color_b_bits, color_a_bits)
        {
            Ok(color) => color,
            Err(code) => return code,
        };

        match with_parley(|parley| {
            // The pinned platform call: PlatformTextSystem::prepare_raster_style
            // (the same call the public TextSystem wrapper makes in
            // crates/gpui/src/text_system.rs:266-279; the wrapper itself is
            // pub(crate) in gpui and not callable from this crate).
            let style = parley
                .prepare_raster_style(RasterStyleRequest { scene_color, requested_mode: mode });
            Ok(style)
        }) {
            Ok(style) => {
                *out = style_record_of(&style);
                glyph_status::OK
            }
            Err(code) => code,
        }
    }
}

/// The stack borrow the platform-trait calls need.
fn with_parley<T>(f: impl FnOnce(&ParleyTextSystem) -> Result<T, i32>) -> Result<T, i32> {
    text::with_stack(|stack| f(&stack.parley))
}

unsafe fn glyph_for_char_body(font_id: u64, char_code: u32, out_glyph_id: *mut u32) -> i32 {
    unsafe {
        if out_glyph_id.is_null() {
            return glyph_status::ERR_NULL_ARG;
        }
        *out_glyph_id = u32::MAX;

        if !text::is_known_font(font_id) {
            return glyph_status::ERR_BAD_HANDLE;
        }
        let Some(ch) = char::from_u32(char_code) else {
            return glyph_status::ERR_BAD_VALUE;
        };

        match with_parley(|parley| {
            let id = FontId(font_id as usize);
            match parley.glyph_for_char(id, ch) {
                Some(glyph) => {
                    Ok(Some(glyph.0))
                }
                None => Ok(None),
            }
        }) {
            Ok(Some(glyph_id)) => {
                *out_glyph_id = glyph_id;
                glyph_status::OK
            }
            Ok(None) => glyph_status::OK,
            Err(code) => code,
        }
    }
}

/// The rasterize body: validates the request against the ABI bounds,
/// calls the pinned public `TextSystem::rasterize_glyph` on the shared
/// stack, encodes the metadata record and dumps the pixel bytes.
unsafe fn glyph_rasterize_body(
    font_id: u64,
    glyph_id: u32,
    font_size_bits: u32,
    subpixel_x: u32,
    subpixel_y: u32,
    scale_bits: u32,
    style: *const GpuiGoRasterStyleRecord,
    out: *mut GpuiGoRasterRecord,
    pixels: *mut u8,
    pixel_capacity: u32,
    out_needed: *mut u32,
) -> i32 {
    unsafe {
        if out.is_null() || out_needed.is_null() {
            return glyph_status::ERR_NULL_ARG;
        }
        let out = &mut *out;
        *out = GpuiGoRasterRecord::default();
        *out_needed = 0;
        if style.is_null() {
            return glyph_status::ERR_NULL_ARG;
        }
        let style = &*style;

        if !text::is_known_font(font_id) {
            return glyph_status::ERR_BAD_HANDLE;
        }
        let style = match style_from_record(style) {
            Ok(style) => style,
            Err(code) => return code,
        };
        let font_size = f32::from_bits(font_size_bits);
        if !font_size.is_finite() || font_size < 0.0 || font_size > MAX_FONT_SIZE {
            return glyph_status::ERR_BAD_VALUE;
        }
        if subpixel_x >= SUBPIXEL_VARIANTS_X as u32 || subpixel_y >= SUBPIXEL_VARIANTS_Y as u32 {
            return glyph_status::ERR_BAD_VALUE;
        }
        let scale = f32::from_bits(scale_bits);
        // The pin's own validation: finite and > 0 (font_rasterizer.rs:603).
        if !scale.is_finite() || scale <= 0.0 || scale > MAX_SCALE {
            return glyph_status::ERR_BAD_VALUE;
        }
        if u32::try_from(glyph_id).is_err() || glyph_id > u16::MAX as u32 {
            return glyph_status::ERR_BAD_VALUE;
        }

        let params = RenderGlyphParams {
            font_id: FontId(font_id as usize),
            glyph_id: GlyphId(glyph_id),
            font_size: px(font_size),
            subpixel_variant: gpui::point(subpixel_x as u8, subpixel_y as u8),
            scale_factor: scale,
            raster_style: style,
        };

        let rasterized = match with_text_system(|system| {
            // The pinned public call: TextSystem::rasterize_glyph
            // (window.rs's paint path), which validates the glyph and
            // checks the raster metadata cache for consistency.
            system
                .rasterize_glyph(&params)
                .map_err(|_e| glyph_status::ERR_RASTER)
        }) {
            Ok(rasterized) => rasterized,
            Err(code) => return code,
        };

        let pixel_count = rasterized.pixels.len() as u32;
        if pixel_count > MAX_RASTER_BYTES {
            return glyph_status::ERR_RASTER;
        }

        *out_needed = pixel_count;
        out.format = match rasterized.format {
            RasterizedGlyphFormat::AlphaMask => raster_format::ALPHA_MASK,
            RasterizedGlyphFormat::BgraSubpixelMask => raster_format::BGRA_SUBPIXEL_MASK,
            RasterizedGlyphFormat::BgraColor => raster_format::BGRA_COLOR,
        };
        out.bounds_x = rasterized.bounds.origin.x.0;
        out.bounds_y = rasterized.bounds.origin.y.0;
        out.bounds_w = rasterized.bounds.size.width.0;
        out.bounds_h = rasterized.bounds.size.height.0;
        out.width = rasterized.size.width.0;
        out.height = rasterized.size.height.0;
        out.pixel_count = pixel_count;
        out.record_size = core::mem::size_of::<GpuiGoRasterRecord>() as u32;

        if pixel_count == 0 {
            return glyph_status::OK;
        }
        if pixel_capacity < pixel_count || pixels.is_null() {
            // The capacity protocol: the metadata record was written; the
            // pixel bytes were not copied.
            return glyph_status::ERR_CAPACITY;
        }
        std::ptr::copy_nonoverlapping(rasterized.pixels.as_ptr(), pixels, pixel_count as usize);
        glyph_status::OK
    }
}

unsafe fn glyph_recommended_mode_body(out_mode: *mut u32) -> i32 {
    unsafe {
        if out_mode.is_null() {
            return glyph_status::ERR_NULL_ARG;
        }
        *out_mode = rendering_mode::PLATFORM_DEFAULT;
        match with_parley(|parley| {
            let mode = parley.recommended_rendering_mode(FontId(0), px(16.0));
            Ok(mode)
        }) {
            Ok(mode) => {
                *out_mode = match mode {
                    TextRenderingMode::PlatformDefault => rendering_mode::PLATFORM_DEFAULT,
                    TextRenderingMode::Subpixel => rendering_mode::SUBPIXEL,
                    TextRenderingMode::Grayscale => rendering_mode::GRAYSCALE,
                };
                glyph_status::OK
            }
            Err(code) => code,
        }
    }
}

unsafe fn glyph_backend_info_body(out: *mut GpuiGoRasterBackendRecord) -> i32 {
    unsafe {
        if out.is_null() {
            return glyph_status::ERR_NULL_ARG;
        }
        // Construct the shared text stack first so the reported facts are
        // the real construction facts, not the pre-construction default.
        if let Err(code) = with_parley(|_parley| Ok(())) {
            return code;
        }
        let facts = backend_facts();
        *out = GpuiGoRasterBackendRecord {
            backend: facts.backend,
            variable_factory: u32::from(facts.variable_factory),
            gamma_bits: facts.gamma.to_bits(),
            contrast_bits: facts.grayscale_enhanced_contrast.to_bits(),
            system_subpixel: u32::from(facts.system_subpixel_rendering),
            record_size: core::mem::size_of::<GpuiGoRasterBackendRecord>() as u32,
        };
        glyph_status::OK
    }
}

// ---------------------------------------------------------------------------
// ABI exports (panic boundaries)
// ---------------------------------------------------------------------------

unsafe extern "system" fn glyph_prepare_style(
    color_r_bits: u32,
    color_g_bits: u32,
    color_b_bits: u32,
    color_a_bits: u32,
    requested_mode: u32,
    out: *mut GpuiGoRasterStyleRecord,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| unsafe {
        glyph_prepare_style_body(
            color_r_bits,
            color_g_bits,
            color_b_bits,
            color_a_bits,
            requested_mode,
            out,
        )
    }));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            if !out.is_null() {
                unsafe {
                    (*out).record_size = 0;
                }
            }
            glyph_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn glyph_for_char(
    font_id: u64,
    char_code: u32,
    out_glyph_id: *mut u32,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| unsafe {
        glyph_for_char_body(font_id, char_code, out_glyph_id)
    }));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            glyph_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn glyph_rasterize(
    font_id: u64,
    glyph_id: u32,
    font_size_bits: u32,
    subpixel_x: u32,
    subpixel_y: u32,
    scale_bits: u32,
    style: *const GpuiGoRasterStyleRecord,
    out: *mut GpuiGoRasterRecord,
    pixels: *mut u8,
    pixel_capacity: u32,
    out_needed: *mut u32,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| unsafe {
        glyph_rasterize_body(
            font_id,
            glyph_id,
            font_size_bits,
            subpixel_x,
            subpixel_y,
            scale_bits,
            style,
            out,
            pixels,
            pixel_capacity,
            out_needed,
        )
    }));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            if !out.is_null() {
                unsafe {
                    (*out).record_size = 0;
                }
            }
            glyph_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn glyph_recommended_mode(out_mode: *mut u32) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| unsafe {
        glyph_recommended_mode_body(out_mode)
    }));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            glyph_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn glyph_backend_info(out: *mut GpuiGoRasterBackendRecord) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| unsafe {
        glyph_backend_info_body(out)
    }));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            glyph_status::ERR_PANIC
        }
    }
}

/// Test-only panic probe: panics inside catch_unwind and reports the
/// contained status.
unsafe extern "system" fn glyph_panic_probe() -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| {
        panic!("glyph service panic probe (contained by design)");
    }));
    match outcome {
        Ok(()) => glyph_status::OK,
        Err(payload) => {
            drop(payload);
            glyph_status::ERR_PANIC
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use gpui::rgba;

    /// Resolves a family through the shared stack the way the text
    /// service does, so raster tests use real canonical FontIds.
    fn resolve(family: &str) -> FontId {
        crate::text::tests::test_resolve_and_register(family)
            .unwrap_or_else(|| panic!("font {family} resolves on this machine"))
    }

    fn raster(
        font_id: FontId,
        glyph_id: GlyphId,
        font_size: f32,
        subpixel: (u32, u32),
        scale: f32,
        mode: u32,
        scene_color: gpui::Rgba,
    ) -> Result<RasterizedGlyph, i32> {
        let style = with_parley(|parley| {
            Ok(parley
                .prepare_raster_style(RasterStyleRequest {
                    scene_color,
                    requested_mode: match mode {
                        raster_mode::GRAYSCALE => GlyphRenderMode::Grayscale,
                        raster_mode::SUBPIXEL => GlyphRenderMode::Subpixel,
                        _ => GlyphRenderMode::Color,
                    },
                }))
        })?;
        let params = RenderGlyphParams {
            font_id,
            glyph_id,
            font_size: px(font_size),
            subpixel_variant: gpui::point(subpixel.0 as u8, subpixel.1 as u8),
            scale_factor: scale,
            raster_style: style,
        };
        with_text_system(|system| {
            system.rasterize_glyph(&params).map_err(|_e| glyph_status::ERR_RASTER)
        })
    }

    fn glyph_for_char(font_id: FontId, ch: char) -> Option<GlyphId> {
        with_parley(|parley| Ok(parley.glyph_for_char(font_id, ch))).unwrap()
    }

    #[test]
    fn backend_is_directwrite_with_recorded_facts() {
        // Constructing the stack selects DirectWrite on this machine and
        // records the facts (the Swash-only path fires only when
        // DirectWrite initialization fails, which does not happen on a
        // normal Windows install with dwrite.dll present).
        let _ = resolve("Segoe UI");
        let facts = backend_facts();
        assert_eq!(facts.backend, raster_backend::DIRECTWRITE);
        assert!(facts.gamma > 0.0, "gamma = {}", facts.gamma);
        assert!(facts.grayscale_enhanced_contrast >= 0.0);
    }

    #[test]
    fn grayscale_and_subpixel_masks_have_the_pinned_formats() {
        let font_id = resolve("Segoe UI");
        let letter = glyph_for_char(font_id, 'A').expect("Segoe UI maps 'A'");

        let gray = raster(font_id, letter, 24.0, (0, 0), 1.0, raster_mode::GRAYSCALE, rgba(0x303030ff))
            .expect("grayscale raster");
        assert_eq!(gray.format, RasterizedGlyphFormat::AlphaMask);
        assert!(gray.bounds.origin.y.0 < 0, "ascent above the baseline: {:?}", gray.bounds);
        assert!(gray.size.width.0 > 0 && gray.size.height.0 > 0);
        assert_eq!(gray.pixels.len(), (gray.size.width.0 * gray.size.height.0) as usize);
        gray.validate().unwrap();

        let subpixel =
            raster(font_id, letter, 24.0, (3, 0), 1.0, raster_mode::SUBPIXEL, rgba(0x303030ff))
                .expect("subpixel raster");
        assert_eq!(subpixel.format, RasterizedGlyphFormat::BgraSubpixelMask);
        assert_eq!(
            subpixel.pixels.len(),
            (subpixel.size.width.0 * subpixel.size.height.0 * 4) as usize
        );
        subpixel.validate().unwrap();
    }

    #[test]
    fn scale_two_doubles_the_mask_extent() {
        let font_id = resolve("Segoe UI");
        let letter = glyph_for_char(font_id, 'g').expect("Segoe UI maps 'g'");
        let one = raster(font_id, letter, 24.0, (0, 0), 1.0, raster_mode::GRAYSCALE, rgba(0x000000ff))
            .expect("scale 1");
        let two = raster(font_id, letter, 24.0, (0, 0), 2.0, raster_mode::GRAYSCALE, rgba(0x000000ff))
            .expect("scale 2");
        assert!(two.size.width.0 > one.size.width.0);
        assert!(two.size.height.0 > one.size.height.0);
    }

    #[test]
    fn color_mode_tints_monochrome_coverage_with_the_prepared_color() {
        let font_id = resolve("Segoe UI");
        let letter = glyph_for_char(font_id, 'A').expect("Segoe UI maps 'A'");
        let color = raster(font_id, letter, 24.0, (1, 0), 1.0, raster_mode::COLOR, rgba(0xe02010cc))
            .expect("color raster");
        assert_eq!(color.format, RasterizedGlyphFormat::BgraColor);
        color.validate().unwrap();
        // Straight alpha with the quantized color: some pixel must be the
        // tinted red (the pin's monochrome currentColor path).
        let tinted = color
            .pixels
            .chunks_exact(4)
            .any(|pixel| pixel[3] > 0 && pixel[2] > pixel[3] && pixel[2] > pixel[0]);
        assert!(tinted, "no tinted pixel in the color raster");
    }

    #[test]
    fn space_glyph_rasterizes_empty() {
        let font_id = resolve("Segoe UI");
        let space = glyph_for_char(font_id, ' ').expect("Segoe UI maps ' '");
        let empty = raster(font_id, space, 24.0, (0, 0), 1.0, raster_mode::GRAYSCALE, rgba(0x000000ff))
            .expect("space raster");
        assert_eq!(empty.size, gpui::Size::default());
        assert!(empty.pixels.is_empty());
        empty.validate().unwrap();
    }

    #[test]
    fn repeated_rasters_are_bit_identical() {
        let font_id = resolve("Segoe UI");
        let letter = glyph_for_char(font_id, 'A').expect("Segoe UI maps 'A'");
        let first = raster(font_id, letter, 16.0, (2, 0), 2.0, raster_mode::GRAYSCALE, rgba(0x000000ff))
            .expect("first raster");
        let second = raster(font_id, letter, 16.0, (2, 0), 2.0, raster_mode::GRAYSCALE, rgba(0x000000ff))
            .expect("second raster");
        assert_eq!(first.bounds, second.bounds);
        assert_eq!(first.format, second.format);
        assert_eq!(first.pixels, second.pixels);
    }

    #[test]
    fn emoji_rasterizes_through_the_recorded_backend_path() {
        // Segoe UI Emoji's family emoji (a color glyph). Whatever kind it
        // carries on this machine (COLRv0/CBDT/COLRv1), the raster must
        // produce a BgraColor buffer or a typed error — never a silent
        // reinterpretation. On Windows 11 this exercises the per-glyph
        // Swash fallback for the typed unsupported kinds.
        let font_id = resolve("Segoe UI Emoji");
        let emoji = match glyph_for_char(font_id, '\u{1F600}') {
            Some(id) => id,
            None => return, // No mapping: the machine lacks the emoji face.
        };
        match raster(font_id, emoji, 24.0, (0, 0), 1.0, raster_mode::COLOR, rgba(0xffffffff)) {
            Ok(glyph) => {
                assert_eq!(glyph.format, RasterizedGlyphFormat::BgraColor);
                glyph.validate().unwrap();
                assert!(
                    glyph.pixels.chunks_exact(4).any(|pixel| {
                        pixel[3] > 128
                            && (pixel[0].abs_diff(pixel[1]) > 20
                                || pixel[1].abs_diff(pixel[2]) > 20
                                || pixel[0].abs_diff(pixel[2]) > 20)
                    }),
                    "no colored pixel in the emoji raster"
                );
            }
            Err(_code) => {
                // A typed raster failure is an honest outcome; the ABI
                // reports ERR_RASTER. The oracle records the same.
            }
        }
    }

    #[test]
    fn style_records_round_trip_the_pinned_styles() {
        let record = with_parley(|parley| {
            let style = parley
                .prepare_raster_style(RasterStyleRequest {
                    scene_color: rgba(0xe02010cc),
                    requested_mode: GlyphRenderMode::Color,
                });
            Ok(style_record_of(&style))
        })
        .unwrap();
        assert_eq!(record.mode, raster_mode::COLOR);
        assert_eq!(record.color_effect_tag, raster_effect::PREBLEND);
        assert_eq!(record.color_r, 0xe0);
        assert_eq!(record.color_g, 0x20);
        assert_eq!(record.color_b, 0x10);
        assert_eq!(record.color_a, 0xcc);
        let decoded = style_from_record(&record).unwrap();
        assert_eq!(decoded.mode, GlyphRenderMode::Color);
        match decoded.color_effect {
            RasterColorEffect::Preblend(color) => {
                assert_eq!(<[u8; 4]>::from(color), [0xe0, 0x20, 0x10, 0xcc]);
            }
            _ => panic!("expected the preblend effect"),
        }
    }

    #[test]
    fn dilation_style_is_rejected() {
        let mut record = GpuiGoRasterStyleRecord::default();
        record.color_effect_tag = raster_effect::DILATION;
        assert_eq!(style_from_record(&record), Err(glyph_status::ERR_BAD_VALUE));
    }

    #[test]
    fn table_self_checks() {
        assert_eq!(GLYPH_TABLE.service_version, GPUI_GO_GLYPH_SERVICE_VERSION);
        assert!(GLYPH_TABLE.prepare_style.is_some());
        assert!(GLYPH_TABLE.glyph_for_char.is_some());
        assert!(GLYPH_TABLE.rasterize.is_some());
        assert!(GLYPH_TABLE.recommended_mode.is_some());
        assert!(GLYPH_TABLE.backend_info.is_some());
        assert!(GLYPH_TABLE.panic_probe.is_some());
        assert_eq!(GLYPH_TABLE.subpixel_variants_x, 4);
        assert_eq!(GLYPH_TABLE.subpixel_variants_y, 1);
    }

    #[test]
    fn panic_probe_reports_containment() {
        assert_eq!(unsafe { glyph_panic_probe() }, glyph_status::ERR_PANIC);
    }
}


