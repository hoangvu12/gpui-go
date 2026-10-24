//! gpui-go native text service (ticket09, reserved slot 4).
//!
//! Shapes text with the **pinned Parley/Fontique text stack** and exposes the
//! paragraph, caret and hit-test geometry gpui's public text API produces:
//! visual lines, paint fragments (shaped runs with resolved font ids),
//! positioned glyphs, carets, selection rectangles, hit tests and logical
//! cluster (grapheme) boundaries. This is CPU geometry only — glyph
//! *rasterization* (DirectWrite/Swash pixel output) is a later ticket and is
//! deliberately not exercised by any entry here.
//!
//! Font registration (the consumer-gaps embedded-font ticket): the
//! `text_font_add` entry registers in-memory font bytes through the pinned
//! `PlatformTextSystem::add_fonts` (the code path behind
//! `TextSystem::add_fonts`), so families embedded in a Go binary resolve by
//! name through the same catalog that shapes and rasterizes. The v2 font
//! record reports `family_found` — whether the descriptor's primary family
//! is present in the catalog — so a family that resolves nowhere (the
//! silent fallback) is diagnosable.
//!
//! # Construction path (the decision the ticket asked to record)
//!
//! The service owns ONE process-global text stack, created lazily behind the
//! service mutex (the same one-mutex serialization the renderer service uses
//! for its device). The construction mirrors the pinned Windows platform
//! (`crates/gpui_windows/src/platform.rs::WindowsPlatform::new`, lines
//! 117-126 at `254b5dbd…`):
//!
//! * `gpui_ce_parley::ParleyTextSystem::new_with_rasterizer(SystemFonts::Load,
//!   "Segoe UI", WindowsGlyphRasterizer::new())` with fallback families
//!   `["Lilex", "IBM Plex Sans", "Arial"]` — the exact platform
//!   construction. Ticket09 recorded a temporary deviation here (the
//!   rasterizer argument was `SwashGlyphRasterizer::default()` because the
//!   pin's `WindowsGlyphRasterizer` is `pub(crate)` in `gpui_windows` and
//!   rasterization was out of that ticket's scope; TEXT_ABI.md recorded
//!   "the glyph-pixels ticket must revisit this"). Ticket10 is that
//!   revisit: `crate::glyph` ports the pinned rasterizer and the stack
//!   now carries it, so the glyph service (reserved slot 5) rasterizes
//!   through the SAME Fontique-backed store that shapes the text — the
//!   pinned face-acquisition mechanism (`PlatformTextSystem::
//!   rasterize_glyph` → `fonts.get(font_id).raster_face(font_id)`). No
//!   geometry entry observes the rasterizer (the fx-0004 trace records
//!   no `is_emoji` flags and no raster output), so the text-geometry
//!   gate stays green while FontIds and metrics stay identical (same
//!   store, same interning).
//! * the concrete `Arc<ParleyTextSystem>` is then wrapped in
//!   `gpui::TextSystem::new(...)` — the goal the ticket states ("a TextSystem
//!   instance with the REAL Parley backend"). `TextSystem::all_font_names` is
//!   served through that instance; the remaining entries call the same
//!   `ParleyTextSystem` through its public `PlatformTextSystem` methods so
//!   failures return typed statuses instead of the gpui wrapper's panics
//!   (`resolve_font` panics; `PlatformTextSystem::font_id` returns
//!   `Result`). The glyph service's rasterize entry additionally calls the
//!   public `TextSystem::rasterize_glyph` (the pinned window paint path's
//!   exact call, including validate and the metadata consistency check).
//!
//! # Pinned shaping semantics (all citations `254b5dbd…`)
//!
//! * **Shaping entry** — `crates/gpui/src/text_system.rs::
//!   WindowTextSystem::shape_text` delegates to
//!   `LineLayoutCache::layout_wrapped_line`, which for any wrapped, clamped
//!   or newline-containing document calls **exactly**
//!   `PlatformTextSystem::layout_text(TextLayoutRequest { text, font_size,
//!   runs, wrap_width, line_clamp })` (and `layout_line` makes the identical
//!   call on cache miss for unwrapped single lines). This service calls
//!   `layout_text` directly: the frame-scoped `LineLayoutCache` belongs to
//!   the Go port per the windows platform contract ("Go retains GPUI style
//!   resolution, text cache keys, measurement requests …"), and a native
//!   cache without `finish_frame` lifecycle would grow unboundedly. The
//!   observable shaping behavior is identical because it is the same call
//!   with the same inputs.
//! * **Run styles** — `crates/gpui_ce_parley/src/text_system.rs::
//!   parley_paragraph_layout`: per-run `FontFamily` (family + descriptor
//!   fallbacks + system fallback + service fallbacks), `FontWeight`,
//!   `FontStyle`, `FontFeatures` (tags validated by `Tag::parse`, values
//!   narrowed to u16) and `LetterSpacing`; default `FontSize`; wrap via
//!   `break_all_lines`/clamped line breaking with
//!   `CHROMIUM_LINE_BREAK_OVERRIDE`.
//! * **Output records** — `crates/gpui/src/text_system/line_layout.rs`:
//!   `LineLayout { font_size, width, ascent, descent, visual_lines,
//!   paint_fragments, len, platform_layout }`, `VisualLine { text_range,
//!   fragment_range, advance_width }`, `PaintFragment { font_id, font_size,
//!   glyphs, x_range }`, `ShapedGlyph { id, position, is_emoji }`.
//! * **Line placement/baseline** — `crates/gpui/src/text_system/line.rs::
//!   paint_visual_text`: line i occupies `[i*line_height, (i+1)*line_height)`
//!   with `baseline_y = i*line_height + (line_height - ascent - descent)/2 +
//!   ascent`; glyph positions are line-local x, baseline-relative y.
//! * **Carets/selection/hit tests** — `crates/gpui/src/text_system/
//!   line_layout.rs::WrappedLineLayout` over `PlatformTextLayout`
//!   (implemented by `gpui_ce_parley`'s `ParleyLayout`/`ParleyDocumentLayout`):
//!   `caret_bounds`, `caret_from_pixel_point`,
//!   `byte_index_from_pixel_point`, `selection_bounds`,
//!   `logical_cluster_before/after`. Logical clusters are **graphemes** in
//!   this pin (`paragraphs.rs` builds them from `grapheme_indices(true)`).
//!
//! # Deviations from the pin (each required by the ABI boundary; none silent)
//!
//! * `CLARIFIED` (output surface): the ticket sketch asked for "per-glyph
//!   advances", "run direction" and "cluster glyph ranges". The pinned
//!   public API exposes **positions** (`ShapedGlyph { id, position,
//!   is_emoji }` — no advance field), no per-run direction on
//!   `LineLayout`/`PaintFragment` (bidi is observable through caret/hit-test
//!   geometry, which is exposed), and no cluster→glyph mapping (clusters are
//!   caret stops). The service exposes exactly what the pin exposes; the pen
//!   advance between consecutive glyphs is derivable from the dumped
//!   positions and each fragment's `x_range` carries the run extent.
//! * `CLARIFIED` (font identity record): the pinned public surface resolves
//!   a `Font` descriptor to the canonical `FontId` (`FontStore::intern`:
//!   `FontId = 0x8000… | store index`). Face index, data identity and
//!   variation axes are `pub(crate)` in `gpui_ce_parley` and not observable
//!   through public APIs; the resolve record therefore carries the FontId
//!   handle, the catalog generation, the echoed descriptor facts and the
//!   full `FontMetrics`. Font handles are process-stable (the store only
//!   interns) and have no dispose.
//! * `CLARIFIED` (validation): the pin assumes validated input from gpui
//!   (`layout_text` even `expect`s internally). The ABI turns those
//!   assumptions into typed caller errors before native entry: run coverage
//!   and UTF-8 boundary checks, feature tag/value checks (the adapter's
//!   `Tag::parse`/u16 narrowing), finite floats, and bounds on text/run/
//!   feature/string capacities. A degenerate-but-finite input the pin
//!   accepts (font size 0, line height 0) stays accepted.
//! * Pre-resolution: `text_shape` resolves every run's font through
//!   `PlatformTextSystem::font_id` before shaping so an unresolvable family
//!   returns `ERR_FONT_UNRESOLVED` instead of the pinned `expect` panic
//!   (which `catch_unwind` would still contain as `ERR_PANIC`).
//!
//! # ABI rules (same conventions as `LAYOUT_ABI.md`/`SCENE_ABI.md`)
//!
//! * C ABI, `extern "system"`, `#[repr(C)]`, fixed-width types; no Rust
//!   strings/Vecs/enums/bools cross the boundary; no Go pointers are
//!   retained by native code (text, run records, feature records and string
//!   buffers are copied into service-owned state; output records and dump
//!   buffers are borrowed for the call only).
//! * Every export contains panics with `catch_unwind`; a contained panic
//!   returns [`text_status::ERR_PANIC`] without poisoning the service (the
//!   state mutex is recovered). There is no latched "failed" state: a failed
//!   entry publishes nothing (shaping slots are filled only after a
//!   successful layout).
//! * Shaping handles encode `(slot index << 20) | slot generation` (the
//!   layout engine's encoding). Stale after dispose or slot reuse; malformed
//!   (out-of-range slot, never-issued generation) is
//!   [`text_status::ERR_BAD_HANDLE`]. Live shaping handles are bounded at
//!   [`MAX_SHAPING_SLOTS`] (256).
//! * Font "handles" are the pinned canonical `FontId` values (bit 63 set,
//!   store index in the low bits). The service validates them against its
//!   registry of resolved and shaping-discovered ids; unknown ids are typed
//!   errors, never the store's `expect`.
//! * Byte offsets are **UTF-8 byte indices** (the layout coordinate system
//!   of the pin). UTF-16 concerns are the Go adapter's; the ABI takes and
//!   returns bytes only.
//! * Bulk dumps (fragments, glyphs, selection rects, font names) use the
//!   capacity protocol: `out_needed` always carries the required count (or
//!   byte count for names), a too-small capacity copies nothing and returns
//!   [`text_status::ERR_CAPACITY`].

use std::collections::HashMap;
use std::panic::{catch_unwind, AssertUnwindSafe};
use std::sync::{Arc, Mutex, MutexGuard};

use gpui::{
    px, Bounds, CaretAffinity, CaretPosition, Font, FontFallbacks, FontFeatures, FontId, FontStyle,
    FontWeight, LineLayout, Pixels, PlatformTextSystem, Point, TextLayoutRequest, TextSystem,
    TextRun,
};
use gpui_ce_parley::{ParleyTextSystem, SystemFonts};

/// Text service ABI version (bumped only on record/table layout changes of
/// the text service). Version 2 (the consumer-gaps font-registration
/// ticket): the font record's v1 `reserved` word became `family_found`, and
/// the table gained the `font_add` entry (slot 15) plus the
/// `max_font_bytes` capacity scalar (200 → 216 bytes).
pub const GPUI_GO_TEXT_SERVICE_VERSION: u32 = 2;

/// Maximum simultaneously live shaping handles. Exceeding the bound fails
/// `text_shape` with [`text_status::ERR_SHAPING_LIMIT`] before any shaping
/// work — the bounded-resource rule every service applies.
pub const MAX_SHAPING_SLOTS: u32 = 256;

/// Maximum accepted UTF-8 text length, in bytes (caller memory is copied
/// into service-owned state; the bound keeps that copy bounded).
pub const MAX_TEXT_BYTES: u32 = 1 << 20;

/// Maximum style runs in one shape request.
pub const MAX_RUNS: u32 = 1024;
/// Maximum variation records in one shape request (rev 15; one array
/// shared by all runs, like the features).
pub const MAX_VARIATIONS: u32 = 4096;

/// Maximum OpenType feature records in one shape/resolve request.
pub const MAX_FEATURES: u32 = 4096;

/// Maximum packed string buffer (family + fallback names), in bytes.
pub const MAX_STRINGS_BYTES: u32 = 64 << 10;

/// Maximum total bytes of the packed family-name dump.
pub const MAX_FONT_NAMES_BYTES: u32 = 1 << 20;

/// Maximum accepted font-data length for one `text_font_add` call
/// (caller memory is copied into service-owned state before registration,
/// so the bound keeps that copy bounded; 64 MiB covers the largest real
/// faces and font collections).
pub const MAX_FONT_BYTES: u32 = 64 << 20;

/// CSS text-wrap tag: nowrap (the run-record values are 0 wrap, 1 nowrap,
/// 2 balance, 3 pretty, 4 stable; rev 15.1 drives the nowrap one — see
/// `nowrap_requested`).
const TEXT_WRAP_NOWRAP: u32 = 1;

/// Text service status codes. 0 is success; negative values are
/// caller/argument errors; 100+ are internal failures.
pub mod text_status {
    /// Success.
    pub const OK: i32 = 0;
    /// The shaping handle encoded a past generation (disposed or slot
    /// reused since).
    pub const ERR_STALE_HANDLE: i32 = -1;
    /// The handle never existed or is malformed (out-of-range slot, a
    /// never-issued generation, a font id without the canonical bit or
    /// outside the registry).
    pub const ERR_BAD_HANDLE: i32 = -2;
    /// A required pointer argument was null.
    pub const ERR_NULL_ARG: i32 = -3;
    /// An invalid tag, enumerant, bound, record size, run coverage, byte
    /// boundary or float in a record (see the module docs' CLARIFIED
    /// validation list).
    pub const ERR_BAD_VALUE: i32 = -4;
    /// Fontique could not resolve the requested family/weight/style (the
    /// pinned `resolve_canonical_font` failure).
    pub const ERR_FONT_UNRESOLVED: i32 = -5;
    /// The live shaping-handle capacity ([`MAX_SHAPING_SLOTS`]) is
    /// exhausted.
    pub const ERR_SHAPING_LIMIT: i32 = -6;
    /// A dump capacity was too small; `out_needed` carries the required
    /// count (records) or byte count (font names).
    pub const ERR_CAPACITY: i32 = -7;
    /// The font bytes submitted to `text_font_add` were rejected: they
    /// contain no supported font face (the pinned `add_fonts` failure —
    /// `register_blobs` bails when the data holds no face).
    pub const ERR_FONT_DATA: i32 = -8;
    /// A native panic was contained by `catch_unwind` during this call.
    pub const ERR_PANIC: i32 = 101;
}

// ---------------------------------------------------------------------------
// Records
// ---------------------------------------------------------------------------

/// One OpenType feature: `tag` is the four ASCII characters of the feature
/// tag packed big-endian (`"liga"` → `0x6C696761`), `value` the setting
/// (0/1 for off/on, at most `u16::MAX` — the adapter narrows to u16).
///
/// Layout (x86-64): `tag` @0, `value` @4; size 8, alignment 4.
#[derive(Clone, Copy)]
#[repr(C)]
pub struct GpuiGoTextFeature {
    /// Feature tag, 4 printable ASCII characters, big-endian.
    pub tag: u32,
    /// Feature value; 0 = off, 1 = on; must fit u16.
    pub value: u32,
}

/// Font resolution request record (caller-owned, borrowed for the call).
///
/// The family name, feature records and packed fallback names arrive in the
/// entry's separate buffers; this record carries the scalars.
///
/// Layout (x86-64): `weight_bits` @0, `style` @4, `feature_count` @8,
/// `fallback_count` @12, `reserved` @16..28; size 28, alignment 4.
#[derive(Clone, Copy)]
#[repr(C)]
pub struct GpuiGoTextFontRequest {
    /// Font weight as f32 bits (100.0..=900.0; finite).
    pub weight_bits: u32,
    /// 0 normal, 1 italic, 2 oblique.
    pub style: u32,
    /// Number of feature records in the features buffer.
    pub feature_count: u32,
    /// Number of NUL-separated fallback family names in the fallbacks
    /// buffer.
    pub fallback_count: u32,
    /// Must be zero (forward compatibility).
    pub reserved: [u32; 3],
}

/// Resolved font identity record (service-written). The family/feature/
/// fallback bytes are the caller's own request and are not echoed; the
/// canonical `FontId` is the pinned resolved identity (store index in the
/// low 63 bits; face index and variation axes are not observable through
/// the pinned public API — see the module docs).
///
/// Layout (x86-64): `font_id` @0 (u64), `generation` @8 (u64),
/// `weight_bits` @16, `style` @20, `feature_count` @24,
/// `fallback_count` @28, `family_found` @32, `record_size` @36; size 40,
/// alignment 8.
#[derive(Clone, Copy)]
#[repr(C)]
pub struct GpuiGoTextFontRecord {
    /// The canonical gpui `FontId` (bit 63 set, store index below).
    pub font_id: u64,
    /// Font catalog generation when the id was registered.
    pub generation: u64,
    /// Echo of the requested weight (f32 bits).
    pub weight_bits: u32,
    /// Echo of the requested style (0/1/2).
    pub style: u32,
    /// Echo of the requested feature count.
    pub feature_count: u32,
    /// Echo of the requested fallback count.
    pub fallback_count: u32,
    /// 1 when the descriptor's primary family is present in the font
    /// catalog (system-installed or registered through `text_font_add`);
    /// 0 when the id resolved through the fallback chain. This is the
    /// port's observability surface for a family that resolves nowhere
    /// (the missing-embedded-font diagnostic): the pinned resolve chain
    /// silently falls back, and the pinned public API exposes no
    /// matched-family report, so the service computes presence against
    /// `all_font_names` (cached per catalog generation).
    pub family_found: u32,
    /// `size_of::<GpuiGoTextFontRecord>()` self-check.
    pub record_size: u32,
}

/// Font metrics record (font units, exactly `gpui::FontMetrics`' fields).
///
/// Layout (x86-64): all fields u32 at 4-byte strides; size 52, alignment 4.
#[derive(Clone, Copy)]
#[repr(C)]
pub struct GpuiGoTextFontMetricsRecord {
    /// Font units per em square.
    pub units_per_em: u32,
    /// Ascent in font units (f32 bits; positive above baseline).
    pub ascent_bits: u32,
    /// Descent in font units (f32 bits; positive below baseline).
    pub descent_bits: u32,
    /// Recommended line gap (f32 bits).
    pub line_gap_bits: u32,
    /// Underline position (f32 bits).
    pub underline_position_bits: u32,
    /// Underline thickness (f32 bits).
    pub underline_thickness_bits: u32,
    /// Capital height (f32 bits).
    pub cap_height_bits: u32,
    /// Lowercase x height (f32 bits).
    pub x_height_bits: u32,
    /// Bounding box origin x (f32 bits).
    pub bbox_x_bits: u32,
    /// Bounding box origin y (f32 bits).
    pub bbox_y_bits: u32,
    /// Bounding box width (f32 bits).
    pub bbox_w_bits: u32,
    /// Bounding box height (f32 bits).
    pub bbox_h_bits: u32,
    /// `size_of::<GpuiGoTextFontMetricsRecord>()` self-check.
    pub record_size: u32,
}

/// One style run of a shape request (caller-owned, borrowed for the call).
/// The family name and fallback names are substrings of the shape entry's
/// packed `strings` buffer (fallbacks NUL-separated within their range);
/// the run's features are a slice of the entry's feature-record array and
/// its variations a slice of the entry's variation-record array (rev 15).
///
/// rev 15 (browser-core ticket 39) grew the record with the browser text
/// fields: the CSS line-breaking controls (word-break, overflow-wrap,
/// text-wrap), the run's locale (a BCP 47 tag substring of the strings
/// buffer, used for locale-sensitive fallback and rendering once the
/// pinned adapter can carry it), the font width ratio and variation
/// settings (CSS font-width / font-variation-settings), the optical size
/// override (CSS font-optical-sizing; absent = auto), and justify
/// (CSS text-align: justify). The pinned `TextLayoutRequest` has no
/// slots for these, so the service validates them, stores them with the
/// shaping handle and echoes them through `text_run_info`; the shaping
/// behavior is unchanged until the fields can be pushed into Parley
/// (recorded honestly in TEXT_ABI.md).
///
/// Layout (x86-64): 23 u32 fields; size 92, alignment 4.
#[repr(C)]
#[derive(Clone, Copy)]
pub struct GpuiGoTextRunRecord {
    /// UTF-8 byte length of this run. Runs must exactly cover the text.
    pub len: u32,
    /// Byte offset of the family name in the strings buffer.
    pub family_offset: u32,
    /// Byte length of the family name.
    pub family_len: u32,
    /// Byte offset of the packed (NUL-separated) fallback names.
    pub fallbacks_offset: u32,
    /// Byte length of the fallback names region (0 = no fallbacks).
    pub fallbacks_len: u32,
    /// Font weight as f32 bits (finite).
    pub weight_bits: u32,
    /// 0 normal, 1 italic, 2 oblique.
    pub style: u32,
    /// Index of this run's first feature record in the features array.
    pub feature_offset: u32,
    /// Number of this run's feature records.
    pub feature_count: u32,
    /// 0/1: whether letter spacing is present for this run.
    pub letter_spacing_present: u32,
    /// Letter spacing in pixels (f32 bits) when present.
    pub letter_spacing_bits: u32,
    /// CSS word-break (parley's WordBreak): 0 normal, 1 break-all,
    /// 2 keep-all.
    pub word_break: u32,
    /// CSS overflow-wrap (parley's OverflowWrap): 0 normal, 1 anywhere,
    /// 2 break-word.
    pub overflow_wrap: u32,
    /// CSS text-wrap: 0 wrap, 1 nowrap, 2 balance, 3 pretty, 4 stable.
    /// rev 15.1: `nowrap` drives the wrap policy (the service suppresses
    /// the soft-wrap constraint); `balance`/`pretty`/`stable` are Go-side
    /// algorithms (bisection) — the tag crosses the ABI for the styling
    /// layer.
    pub text_wrap: u32,
    /// Byte offset of the locale (a BCP 47 language tag) in the strings
    /// buffer; with `locale_len` 0 the run has no locale.
    pub locale_offset: u32,
    /// Byte length of the locale (0 = none).
    pub locale_len: u32,
    /// 0/1: whether a font width is present for this run.
    pub font_width_present: u32,
    /// Font width ratio (f32 bits; 1.0 = normal, CSS font-width) when
    /// present.
    pub font_width_bits: u32,
    /// Index of this run's first variation record in the variations
    /// array.
    pub variation_offset: u32,
    /// Number of this run's variation records.
    pub variation_count: u32,
    /// 0/1: whether an optical size override is present (absent = the
    /// CSS `auto` behavior).
    pub opsz_present: u32,
    /// Optical size in pixels (f32 bits) when present.
    pub opsz_bits: u32,
    /// 0 = no justification, 1 = justify (CSS text-align: justify).
    pub justify: u32,
}

/// One font variation setting (rev 15; parley's `FontVariation`):
/// an OpenType axis tag with a float value.
///
/// Layout (x86-64): `tag` @0, `value` @4; size 8, alignment 4.
#[repr(C)]
#[derive(Clone, Copy)]
pub struct GpuiGoTextVariation {
    /// Variation axis tag, 4 printable ASCII characters, big-endian
    /// (the same rule as the feature tags).
    pub tag: u32,
    /// The variation value (f32 bits; finite).
    pub value: u32,
}

/// Shaped layout summary (service-written).
///
/// Layout (x86-64): 14 u32 fields; size 56, alignment 4.
#[derive(Clone, Copy)]
#[repr(C)]
pub struct GpuiGoTextLayoutRecord {
    /// UTF-8 length of the shaped text.
    pub text_len: u32,
    /// Font size the document was shaped with (f32 bits).
    pub font_size_bits: u32,
    /// 0/1: whether a wrap constraint is active.
    pub wrap_present: u32,
    /// Wrap width in pixels (f32 bits) when present.
    pub wrap_bits: u32,
    /// 0/1: whether a line clamp is active.
    pub clamp_present: u32,
    /// Maximum visual rows when present.
    pub line_clamp: u32,
    /// Number of visual lines (`LineLayout::visual_lines.len()`).
    pub line_count: u32,
    /// Number of paint fragments (shaped runs).
    pub fragment_count: u32,
    /// Total positioned glyphs across all fragments.
    pub glyph_count: u32,
    /// `LineLayout::width` — the widest visual line's advance (f32 bits).
    pub width_bits: u32,
    /// `LineLayout::ascent` (f32 bits).
    pub ascent_bits: u32,
    /// `LineLayout::descent` (f32 bits).
    pub descent_bits: u32,
    /// Font catalog generation at layout time.
    pub font_generation: u32,
    /// `size_of::<GpuiGoTextLayoutRecord>()` self-check.
    pub record_size: u32,
}

/// One visual line record (service-written). The row occupies
/// `[line_index * line_height, (line_index + 1) * line_height)` vertically
/// and `[0, advance_width)` horizontally before paint-time alignment; the
/// baseline formula is the pinned `paint_visual_text` derivation
/// `(line_height - ascent - descent) / 2 + ascent` (see module docs).
///
/// Layout (x86-64): 9 u32 fields; size 36, alignment 4.
#[derive(Clone, Copy)]
#[repr(C)]
pub struct GpuiGoTextLineRecord {
    /// Echo of the queried line index.
    pub line_index: u32,
    /// Logical UTF-8 byte range start of this row.
    pub text_start: u32,
    /// Logical UTF-8 byte range end of this row.
    pub text_end: u32,
    /// First paint-fragment index of this row.
    pub fragment_start: u32,
    /// One-past-last paint-fragment index of this row.
    pub fragment_end: u32,
    /// Horizontal advance of the row's shaped content (f32 bits).
    pub advance_width_bits: u32,
    /// Echo of the query's line height (f32 bits).
    pub line_height_bits: u32,
    /// Baseline offset from the row's top edge (f32 bits).
    pub baseline_bits: u32,
    /// `size_of::<GpuiGoTextLineRecord>()` self-check.
    pub record_size: u32,
}

/// Caret record for a byte position (service-written). `present` is 0 when
/// the pinned `caret_bounds` returns `None` (the geometry is then
/// zeroed); the two cluster records carry the grapheme ranges adjacent to
/// the caret (the pin's `logical_cluster_before`/`after`).
///
/// Layout (x86-64): 14 u32 fields; size 56, alignment 4.
#[derive(Clone, Copy)]
#[repr(C)]
pub struct GpuiGoTextCaretRecord {
    /// The caret's UTF-8 byte index (echo).
    pub index: u32,
    /// 0 downstream, 1 upstream (echo).
    pub affinity: u32,
    /// 0/1: whether caret geometry exists.
    pub present: u32,
    /// Caret bounds x (f32 bits).
    pub x_bits: u32,
    /// Caret bounds y (f32 bits).
    pub y_bits: u32,
    /// Caret bounds width (f32 bits).
    pub w_bits: u32,
    /// Caret bounds height (f32 bits).
    pub h_bits: u32,
    /// 0/1: whether a preceding logical cluster exists.
    pub cluster_before_present: u32,
    /// Preceding cluster byte range start.
    pub cluster_before_start: u32,
    /// Preceding cluster byte range end.
    pub cluster_before_end: u32,
    /// 0/1: whether a following logical cluster exists.
    pub cluster_after_present: u32,
    /// Following cluster byte range start.
    pub cluster_after_start: u32,
    /// Following cluster byte range end.
    pub cluster_after_end: u32,
    /// `size_of::<GpuiGoTextCaretRecord>()` self-check.
    pub record_size: u32,
}

/// Hit-test record (service-written). `inside` is 1 when the point fell
/// inside a visual row (the pinned `Ok`), 0 when it was outside and the
/// record carries the edge caret (the pinned `Err`).
///
/// Layout (x86-64): 4 u32 fields; size 16, alignment 4.
#[derive(Clone, Copy)]
#[repr(C)]
pub struct GpuiGoTextHitRecord {
    /// The closest caret's UTF-8 byte index.
    pub index: u32,
    /// 0 downstream, 1 upstream.
    pub affinity: u32,
    /// 0/1: whether the point was inside a visual row.
    pub inside: u32,
    /// `size_of::<GpuiGoTextHitRecord>()` self-check.
    pub record_size: u32,
}

/// One selection rectangle (service-written), a bulk-dump record.
///
/// Layout (x86-64): 5 u32 fields; size 20, alignment 4.
#[derive(Clone, Copy)]
#[repr(C)]
pub struct GpuiGoTextSelectionRecord {
    /// Bounds x (f32 bits).
    pub x_bits: u32,
    /// Bounds y (f32 bits).
    pub y_bits: u32,
    /// Bounds width (f32 bits).
    pub w_bits: u32,
    /// Bounds height (f32 bits).
    pub h_bits: u32,
    /// `size_of::<GpuiGoTextSelectionRecord>()` self-check.
    pub record_size: u32,
}

/// Logical cluster (grapheme) record for a byte position (service-written).
///
/// Layout (x86-64): 4 u32 fields; size 16, alignment 4.
#[derive(Clone, Copy)]
#[repr(C)]
pub struct GpuiGoTextClusterRecord {
    /// 0/1: whether the requested side has a cluster.
    pub present: u32,
    /// Cluster byte range start.
    pub start: u32,
    /// Cluster byte range end.
    pub end: u32,
    /// `size_of::<GpuiGoTextClusterRecord>()` self-check.
    pub record_size: u32,
}

/// One paint fragment (shaped run) record, a bulk-dump record. Glyphs
/// occupy the dump's indices `[glyph_start, glyph_start + glyph_count)`.
///
/// Layout (x86-64): `font_id` @0 (u64), then 5 u32 fields; size 32,
/// alignment 8.
#[derive(Clone, Copy)]
#[repr(C)]
pub struct GpuiGoTextFragmentRecord {
    /// The resolved canonical `FontId` of this run's glyphs.
    pub font_id: u64,
    /// The run's resolved font size (f32 bits).
    pub font_size_bits: u32,
    /// Fragment x-range start in the line (f32 bits).
    pub x_start_bits: u32,
    /// Fragment x-range end in the line (f32 bits).
    pub x_end_bits: u32,
    /// Index of the fragment's first glyph in the glyph dump.
    pub glyph_start: u32,
    /// Number of glyphs in this fragment.
    pub glyph_count: u32,
    /// `size_of::<GpuiGoTextFragmentRecord>()` self-check.
    pub record_size: u32,
}

/// One positioned glyph record, a bulk-dump record. `x` is line-local and
/// `y` is relative to the line's **baseline** (the painter adds
/// `baseline_y + position.y`), exactly as the pinned `ShapedGlyph` carries
/// it.
///
/// Layout (x86-64): 5 u32 fields; size 20, alignment 4.
#[derive(Clone, Copy, Debug, PartialEq)]
#[repr(C)]
pub struct GpuiGoTextGlyphRecord {
    /// The glyph id (`GlyphId.0`).
    pub glyph_id: u32,
    /// Position x (f32 bits, line-local).
    pub x_bits: u32,
    /// Position y (f32 bits, baseline-relative).
    pub y_bits: u32,
    /// 0/1: whether this glyph is color artwork the rasterizer supports.
    pub is_emoji: u32,
    /// `size_of::<GpuiGoTextGlyphRecord>()` self-check.
    pub record_size: u32,
}

// Compile-time layout pins (the Go loader mirrors these sizes exactly).
const _: () = assert!(std::mem::size_of::<GpuiGoTextFeature>() == 8);
const _: () = assert!(std::mem::align_of::<GpuiGoTextFeature>() == 4);
const _: () = assert!(std::mem::size_of::<GpuiGoTextFontRequest>() == 28);
const _: () = assert!(std::mem::align_of::<GpuiGoTextFontRequest>() == 4);
const _: () = assert!(std::mem::size_of::<GpuiGoTextFontRecord>() == 40);
const _: () = assert!(std::mem::align_of::<GpuiGoTextFontRecord>() == 8);
const _: () = assert!(std::mem::size_of::<GpuiGoTextFontMetricsRecord>() == 52);
const _: () = assert!(std::mem::align_of::<GpuiGoTextFontMetricsRecord>() == 4);
const _: () = assert!(std::mem::size_of::<GpuiGoTextRunRecord>() == 92);
const _: () = assert!(std::mem::align_of::<GpuiGoTextRunRecord>() == 4);
const _: () = assert!(std::mem::size_of::<GpuiGoTextVariation>() == 8);
const _: () = assert!(std::mem::align_of::<GpuiGoTextVariation>() == 4);
const _: () = assert!(std::mem::size_of::<GpuiGoTextLayoutRecord>() == 56);
const _: () = assert!(std::mem::align_of::<GpuiGoTextLayoutRecord>() == 4);
const _: () = assert!(std::mem::size_of::<GpuiGoTextLineRecord>() == 36);
const _: () = assert!(std::mem::align_of::<GpuiGoTextLineRecord>() == 4);
const _: () = assert!(std::mem::size_of::<GpuiGoTextCaretRecord>() == 56);
const _: () = assert!(std::mem::align_of::<GpuiGoTextCaretRecord>() == 4);
const _: () = assert!(std::mem::size_of::<GpuiGoTextHitRecord>() == 16);
const _: () = assert!(std::mem::align_of::<GpuiGoTextHitRecord>() == 4);
const _: () = assert!(std::mem::size_of::<GpuiGoTextSelectionRecord>() == 20);
const _: () = assert!(std::mem::align_of::<GpuiGoTextSelectionRecord>() == 4);
const _: () = assert!(std::mem::size_of::<GpuiGoTextClusterRecord>() == 16);
const _: () = assert!(std::mem::align_of::<GpuiGoTextClusterRecord>() == 4);
const _: () = assert!(std::mem::size_of::<GpuiGoTextFragmentRecord>() == 32);
const _: () = assert!(std::mem::align_of::<GpuiGoTextFragmentRecord>() == 8);
const _: () = assert!(std::mem::size_of::<GpuiGoTextGlyphRecord>() == 20);
const _: () = assert!(std::mem::align_of::<GpuiGoTextGlyphRecord>() == 4);

// ---------------------------------------------------------------------------
// Service table
// ---------------------------------------------------------------------------

/// The text service table: 16 function pointers then 21 self-check/capacity
/// scalars; size 216, alignment 8. Installed in reserved slot 4 of the
/// bootstrap table and advertised with capability bit 4
/// (`text-parley-0-11-1`).
#[derive(Clone, Copy)]
#[repr(C)]
pub struct GpuiGoTextTable {
    /// `text_font_names`.
    pub font_names: Option<
        unsafe extern "system" fn(*mut u8, u32, *mut u32, *mut u32) -> i32,
    >,
    /// `text_font_resolve`.
    pub font_resolve: Option<
        unsafe extern "system" fn(
            *const GpuiGoTextFontRequest,
            *const u8,
            u32,
            *const GpuiGoTextFeature,
            u32,
            *const u8,
            u32,
            *mut GpuiGoTextFontRecord,
        ) -> i32,
    >,
    /// `text_font_metrics`.
    pub font_metrics:
        Option<unsafe extern "system" fn(u64, *mut GpuiGoTextFontMetricsRecord) -> i32>,
    /// `text_shape` (rev 15: the variation array joins the feature
    /// array).
    pub shape: Option<
        unsafe extern "system" fn(
            *const u8,
            u32,
            *const GpuiGoTextRunRecord,
            u32,
            *const GpuiGoTextFeature,
            u32,
            *const GpuiGoTextVariation,
            u32,
            *const u8,
            u32,
            u32,
            u32,
            u32,
            u32,
            u32,
            *mut u64,
        ) -> i32,
    >,
    /// `text_run_info` (rev 15): echoes one stored run record (the
    /// browser text fields the pinned `TextRun` cannot carry).
    pub run_info: Option<
        unsafe extern "system" fn(u64, u32, *mut GpuiGoTextRunRecord) -> i32,
    >,
    /// `text_layout` (re-layout under a new wrap/clamp constraint).
    pub layout:
        Option<unsafe extern "system" fn(u64, u32, u32, u32, u32) -> i32>,
    /// `text_dispose`.
    pub dispose: Option<unsafe extern "system" fn(u64) -> i32>,
    /// `text_layout_info`.
    pub layout_info:
        Option<unsafe extern "system" fn(u64, *mut GpuiGoTextLayoutRecord) -> i32>,
    /// `text_line_count`.
    pub line_count: Option<unsafe extern "system" fn(u64, *mut u32) -> i32>,
    /// `text_line`.
    pub line:
        Option<unsafe extern "system" fn(u64, u32, u32, *mut GpuiGoTextLineRecord) -> i32>,
    /// `text_caret`.
    pub caret:
        Option<unsafe extern "system" fn(u64, u32, u32, u32, *mut GpuiGoTextCaretRecord) -> i32>,
    /// `text_hit_test`.
    pub hit_test:
        Option<unsafe extern "system" fn(u64, u32, u32, u32, *mut GpuiGoTextHitRecord) -> i32>,
    /// `text_selection_rects`.
    pub selection_rects: Option<
        unsafe extern "system" fn(
            u64,
            u32,
            u32,
            u32,
            *mut GpuiGoTextSelectionRecord,
            u32,
            *mut u32,
        ) -> i32,
    >,
    /// `text_cluster`.
    pub cluster:
        Option<unsafe extern "system" fn(u64, u32, u32, u32, *mut GpuiGoTextClusterRecord) -> i32>,
    /// `text_fragments`.
    pub fragments: Option<
        unsafe extern "system" fn(u64, *mut GpuiGoTextFragmentRecord, u32, *mut u32) -> i32,
    >,
    /// `text_glyphs`.
    pub glyphs: Option<
        unsafe extern "system" fn(u64, *mut GpuiGoTextGlyphRecord, u32, *mut u32) -> i32,
    >,
    /// `text_font_add` (the pinned `TextSystem::add_fonts`: register font
    /// bytes; the families appear in `text_font_names` and resolve by
    /// name).
    pub font_add: Option<
        unsafe extern "system" fn(data: *const u8, data_len: u32, out_generation: *mut u64) -> i32,
    >,
    /// Text ABI version (see [`GPUI_GO_TEXT_SERVICE_VERSION`]).
    pub service_version: u32,
    /// `size_of::<GpuiGoTextTable>()` self-check.
    pub size_of_table: u32,
    /// `align_of::<GpuiGoTextTable>()` self-check.
    pub align_of_table: u32,
    /// `size_of::<GpuiGoTextFontRequest>()` self-check.
    pub size_of_font_request: u32,
    /// `size_of::<GpuiGoTextFontRecord>()` self-check.
    pub size_of_font_record: u32,
    /// `align_of::<GpuiGoTextFontRecord>()` self-check (8).
    pub align_of_font_record: u32,
    /// `size_of::<GpuiGoTextFontMetricsRecord>()` self-check.
    pub size_of_metrics_record: u32,
    /// `size_of::<GpuiGoTextRunRecord>()` self-check.
    pub size_of_run_record: u32,
    /// `size_of::<GpuiGoTextFeature>()` self-check.
    pub size_of_feature_record: u32,
    /// `size_of::<GpuiGoTextLayoutRecord>()` self-check.
    pub size_of_layout_record: u32,
    /// `size_of::<GpuiGoTextLineRecord>()` self-check.
    pub size_of_line_record: u32,
    /// `size_of::<GpuiGoTextCaretRecord>()` self-check.
    pub size_of_caret_record: u32,
    /// `size_of::<GpuiGoTextHitRecord>()` self-check.
    pub size_of_hit_record: u32,
    /// `size_of::<GpuiGoTextSelectionRecord>()` self-check.
    pub size_of_selection_record: u32,
    /// `size_of::<GpuiGoTextClusterRecord>()` self-check.
    pub size_of_cluster_record: u32,
    /// `size_of::<GpuiGoTextFragmentRecord>()` self-check.
    pub size_of_fragment_record: u32,
    /// `align_of::<GpuiGoTextFragmentRecord>()` self-check (8).
    pub align_of_fragment_record: u32,
    /// `size_of::<GpuiGoTextGlyphRecord>()` self-check.
    pub size_of_glyph_record: u32,
    /// Live shaping-handle bound ([`MAX_SHAPING_SLOTS`]).
    pub max_shaping_handles: u32,
    /// Text length bound ([`MAX_TEXT_BYTES`]).
    pub max_text_bytes: u32,
    /// Font-data length bound for one `text_font_add` call
    /// ([`MAX_FONT_BYTES`]).
    pub max_font_bytes: u32,
}

const _: () = assert!(std::mem::size_of::<GpuiGoTextTable>() == 224);
const _: () = assert!(std::mem::align_of::<GpuiGoTextTable>() == 8);

/// The one static text service table. Its address is stable for the process
/// lifetime; the DLL never frees or mutates it.
pub(crate) static TEXT_TABLE: GpuiGoTextTable = GpuiGoTextTable {
    font_names: Some(text_font_names),
    font_resolve: Some(text_font_resolve),
    font_metrics: Some(text_font_metrics),
    shape: Some(text_shape),
    run_info: Some(text_run_info),
    layout: Some(text_layout),
    dispose: Some(text_dispose),
    layout_info: Some(text_layout_info),
    line_count: Some(text_line_count),
    line: Some(text_line),
    caret: Some(text_caret),
    hit_test: Some(text_hit_test),
    selection_rects: Some(text_selection_rects),
    cluster: Some(text_cluster),
    fragments: Some(text_fragments),
    glyphs: Some(text_glyphs),
    font_add: Some(text_font_add),
    service_version: GPUI_GO_TEXT_SERVICE_VERSION,
    size_of_table: std::mem::size_of::<GpuiGoTextTable>() as u32,
    align_of_table: std::mem::align_of::<GpuiGoTextTable>() as u32,
    size_of_font_request: std::mem::size_of::<GpuiGoTextFontRequest>() as u32,
    size_of_font_record: std::mem::size_of::<GpuiGoTextFontRecord>() as u32,
    align_of_font_record: std::mem::align_of::<GpuiGoTextFontRecord>() as u32,
    size_of_metrics_record: std::mem::size_of::<GpuiGoTextFontMetricsRecord>() as u32,
    size_of_run_record: std::mem::size_of::<GpuiGoTextRunRecord>() as u32,
    size_of_feature_record: std::mem::size_of::<GpuiGoTextFeature>() as u32,
    size_of_layout_record: std::mem::size_of::<GpuiGoTextLayoutRecord>() as u32,
    size_of_line_record: std::mem::size_of::<GpuiGoTextLineRecord>() as u32,
    size_of_caret_record: std::mem::size_of::<GpuiGoTextCaretRecord>() as u32,
    size_of_hit_record: std::mem::size_of::<GpuiGoTextHitRecord>() as u32,
    size_of_selection_record: std::mem::size_of::<GpuiGoTextSelectionRecord>() as u32,
    size_of_cluster_record: std::mem::size_of::<GpuiGoTextClusterRecord>() as u32,
    size_of_fragment_record: std::mem::size_of::<GpuiGoTextFragmentRecord>() as u32,
    align_of_fragment_record: std::mem::align_of::<GpuiGoTextFragmentRecord>() as u32,
    size_of_glyph_record: std::mem::size_of::<GpuiGoTextGlyphRecord>() as u32,
    max_shaping_handles: MAX_SHAPING_SLOTS,
    max_text_bytes: MAX_TEXT_BYTES,
    max_font_bytes: MAX_FONT_BYTES,
};

// ---------------------------------------------------------------------------
// Handle packing and global state
// ---------------------------------------------------------------------------

/// Shaping handle: `(slot index << 20) | slot generation`, slot generation
/// in the low 20 bits (the layout engine's encoding).
const SHAPING_HANDLE_SLOT_SHIFT: u32 = 20;
/// Maximum slot generation (20 bits).
const SHAPING_GENERATION_MAX: u32 = 0x000F_FFFF;

fn shaping_handle_parts(handle: u64) -> (usize, u32) {
    (
        (handle >> SHAPING_HANDLE_SLOT_SHIFT) as usize,
        (handle & SHAPING_GENERATION_MAX as u64) as u32,
    )
}

fn make_shaping_handle(slot: usize, generation: u32) -> u64 {
    ((slot as u64) << SHAPING_HANDLE_SLOT_SHIFT) | generation as u64
}

/// The pinned canonical FontId's high bit (`FontStore::intern`:
/// `FontId(CANONICAL_FONT_ID_BIT | index)` with
/// `CANONICAL_FONT_ID_BIT = 1 << 63`).
const FONT_ID_CANONICAL_BIT: u64 = 1 << 63;

/// One live shaping document: the shaping inputs (for re-layout) plus the
/// current layout result.
struct ShapingState {
    /// The UTF-8 source text (service-owned copy).
    text: String,
    /// The run list in the pinned descriptor form (service-owned copy).
    runs: Vec<TextRun>,
    /// The run records as they crossed the ABI (service-owned copy; the
    /// rev-15 browser text fields — word-break, overflow-wrap,
    /// text-wrap, locale, font width, variations, optical size, justify
    /// — have no pinned `TextRun` slots, so they are stored here and
    /// echoed through `text_run_info` until the pinned adapter can
    /// carry them).
    run_records: Vec<GpuiGoTextRunRecord>,
    /// The document's font size.
    font_size: Pixels,
    /// The active wrap constraint.
    wrap_width: Option<Pixels>,
    /// The active line clamp.
    line_clamp: Option<usize>,
    /// The current layout (the pinned `layout_text` result).
    layout: Arc<LineLayout>,
    /// Font catalog generation when the layout was produced.
    font_generation: u64,
}

struct ShapingSlot {
    /// Slot generation of the last document that occupied this slot
    /// (persists while vacant so disposed handles stay stale after slot
    /// reuse).
    slot_generation: u32,
    occupied: bool,
    shaping: Option<ShapingState>,
}

impl ShapingSlot {
    fn vacant() -> Self {
        ShapingSlot { slot_generation: 0, occupied: false, shaping: None }
    }
}

/// The pinned text stack, created once per process.
#[derive(Clone)]
pub(super) struct TextStack {
    /// The pinned Parley-based `PlatformTextSystem` (concrete type: typed
    /// `font_id`/`font_metrics`/`layout_text` calls, plus the
    /// DirectWrite-first rasterizer injected at construction).
    pub(super) parley: Arc<ParleyTextSystem>,
    /// The gpui `TextSystem` wrapping the same instance (the public
    /// wrapper; `all_font_names` and `rasterize_glyph` are served through
    /// it).
    pub(super) system: Arc<TextSystem>,
}

/// The whole text service state behind one mutex. The mutex is the
/// serialization boundary: every export takes it, and no lock is ever held
/// across a callback (the service has none).
struct TextServiceState {
    stack: Option<TextStack>,
    /// Registry of known canonical FontIds (resolved through
    /// `text_font_resolve` or discovered in shaping output) → catalog
    /// generation at registration. Metrics queries validate against this
    /// instead of relying on the store's `expect`.
    fonts: HashMap<u64, u64>,
    /// Cache of the catalog's family-name set, keyed by the catalog
    /// generation it was built from (`all_font_names` enumerates and sorts
    /// the whole catalog, so the resolve path must not call it per
    /// resolve). Invalidated by `text_font_add` (the only generation
    /// mutator); rebuilt lazily on the next `family_found` query.
    family_names: Option<(u64, std::collections::HashSet<String>)>,
    /// gpui-perf ticket 05 / zterm ticket 13: the effective weight of
    /// each requested (family, fallbacks, weight bits, style), so the face
    /// probe runs once per combination. Cleared by
    /// `text_font_add` (a new face can change the selection).
    weight_fixups: HashMap<WeightKey, u32>,
    slots: Vec<ShapingSlot>,
}

/// Cache key of [`TextServiceState::weight_fixups`].
type WeightKey = (String, Vec<String>, u32, u8);

static GLOBAL: std::sync::LazyLock<Mutex<TextServiceState>> =
    std::sync::LazyLock::new(|| {
        Mutex::new(TextServiceState {
            stack: None,
            fonts: HashMap::new(),
            family_names: None,
            weight_fixups: HashMap::new(),
            slots: Vec::new(),
        })
    });

fn lock_global() -> MutexGuard<'static, TextServiceState> {
    GLOBAL.lock().unwrap_or_else(|poisoned| poisoned.into_inner())
}

/// Creates the pinned text stack on first use (the pinned Windows platform
/// construction with the DirectWrite-first rasterizer ported in
/// `crate::glyph`; see the module docs for the ticket10 revisit).
fn ensure_stack(state: &mut TextServiceState) -> TextStack {
    if state.stack.is_none() {
        let parley = Arc::new(
            ParleyTextSystem::new_with_rasterizer(
                SystemFonts::Load,
                "Segoe UI",
                crate::glyph::new_windows_rasterizer(),
            )
            .with_fallback_families(["Lilex", "IBM Plex Sans", "Arial"]),
        );
        // `TextSystem::new` takes `Arc<dyn PlatformTextSystem>`; the clone
        // shares the same instance with `parley`.
        let system = Arc::new(TextSystem::new(parley.clone()));
        state.stack = Some(TextStack { parley, system });
    }
    state.stack.clone().expect("stack was just ensured")
}

/// Runs `f` with the process-global text stack, locked and constructed on
/// first use. Ticket10's glyph service borrows the SAME stack (the pinned
/// face-acquisition mechanism: one Fontique-backed store for shaping and
/// rasterization). No lock is held across a callback.
pub(super) fn with_stack<T>(f: impl FnOnce(&TextStack) -> Result<T, i32>) -> Result<T, i32> {
    let mut state = lock_global();
    let stack = ensure_stack(&mut state);
    f(&stack)
}

/// Whether a canonical font id is known to the service's registry (resolved
/// through `text_font_resolve` or discovered in shaping output). The glyph
/// service validates its rasterize/glyph-for-char font ids through this.
pub(super) fn is_known_font(font_id: u64) -> bool {
    let state = lock_global();
    state.fonts.contains_key(&font_id)
}

/// Registers a canonical FontId in the font registry (idempotent).
fn register_font(state: &mut TextServiceState, stack: &TextStack, font_id: FontId) {
    let key = font_id.0 as u64;
    if !state.fonts.contains_key(&key) {
        let generation = stack.parley.font_generation();
        state.fonts.insert(key, generation);
    }
}

/// Whether the descriptor's primary family is present in the font catalog
/// (`all_font_names`: system families plus families registered through
/// `text_font_add`, plus the `.SystemUIFont` alias). The set is cached per
/// catalog generation; `text_font_add` bumps the generation, so the cache
/// stays correct across registrations.
fn family_present(state: &mut TextServiceState, stack: &TextStack, family: &str) -> bool {
    let generation = stack.parley.font_generation();
    let cached = state
        .family_names
        .as_ref()
        .is_some_and(|(cached_generation, _)| *cached_generation == generation);
    if !cached {
        let names: std::collections::HashSet<String> =
            stack.system.all_font_names().into_iter().collect();
        state.family_names = Some((generation, names));
    }
    state
        .family_names
        .as_ref()
        .expect("the family-name cache was just ensured")
        .1
        .contains(family)
}

// ---------------------------------------------------------------------------
// Input validation and translation (the CLARIFIED validation list)
// ---------------------------------------------------------------------------

/// Finite f32 from bits (NaN/inf rejected; see the module docs).
fn finite_f32(bits: u32) -> Result<f32, i32> {
    let value = f32::from_bits(bits);
    if value.is_finite() {
        Ok(value)
    } else {
        Err(text_status::ERR_BAD_VALUE)
    }
}

fn weight_from_bits(bits: u32) -> Result<FontWeight, i32> {
    Ok(FontWeight(finite_f32(bits)?))
}

fn style_from_u32(style: u32) -> Result<FontStyle, i32> {
    match style {
        0 => Ok(FontStyle::Normal),
        1 => Ok(FontStyle::Italic),
        2 => Ok(FontStyle::Oblique),
        _ => Err(text_status::ERR_BAD_VALUE),
    }
}

/// Borrows `len` bytes from `ptr` (null allowed only for len 0).
unsafe fn borrowed_slice<'a>(ptr: *const u8, len: u32) -> Result<&'a [u8], i32> {
    if len == 0 {
        return Ok(&[]);
    }
    if ptr.is_null() {
        return Err(text_status::ERR_NULL_ARG);
    }
    Ok(unsafe { std::slice::from_raw_parts(ptr, len as usize) })
}

/// A borrowed record array (null allowed only for count 0).
unsafe fn borrowed_records<'a, T>(ptr: *const T, count: u32) -> Result<&'a [T], i32> {
    if count == 0 {
        return Ok(&[]);
    }
    if ptr.is_null() {
        return Err(text_status::ERR_NULL_ARG);
    }
    Ok(unsafe { std::slice::from_raw_parts(ptr, count as usize) })
}

/// Validates one OpenType feature record (the adapter's `Tag::parse` +
/// u16 narrowing, as typed caller errors).
fn check_feature(feature: &GpuiGoTextFeature) -> Result<(), i32> {
    let tag = feature.tag;
    for shift in [24u32, 16, 8, 0] {
        let byte = (tag >> shift) & 0xFF;
        if !(0x20..=0x7E).contains(&byte) {
            return Err(text_status::ERR_BAD_VALUE);
        }
    }
    if feature.value > u16::MAX as u32 {
        return Err(text_status::ERR_BAD_VALUE);
    }
    Ok(())
}

/// Validates one variation record (rev 15): a printable-ASCII axis tag
/// (the feature tags' rule) with a finite value.
fn check_variation(variation: &GpuiGoTextVariation) -> Result<(), i32> {
    let tag = variation.tag;
    for shift in [24u32, 16, 8, 0] {
        let byte = (tag >> shift) & 0xFF;
        if !(0x20..=0x7E).contains(&byte) {
            return Err(text_status::ERR_BAD_VALUE);
        }
    }
    if !f32::from_bits(variation.value).is_finite() {
        return Err(text_status::ERR_BAD_VALUE);
    }
    Ok(())
}

/// Decodes a feature record into the pinned `(String, u32)` pair form.
fn feature_pair(feature: &GpuiGoTextFeature) -> (String, u32) {
    let bytes = feature.tag.to_be_bytes();
    let tag = bytes.iter().map(|&b| b as char).collect::<String>();
    (tag, feature.value)
}

/// Splits a packed NUL-separated name region into owned strings, validating
/// UTF-8 and rejecting empty segments (a NUL-terminated run of names: the
/// caller packs `name\0name\0`; the region may omit the trailing NUL).
fn split_names(region: &[u8], expected_count: usize) -> Result<Vec<String>, i32> {
    let mut names = Vec::with_capacity(expected_count);
    let mut rest = region;
    while !rest.is_empty() {
        let end = rest.iter().position(|&b| b == 0).unwrap_or(rest.len());
        let name = std::str::from_utf8(&rest[..end]).map_err(|_| text_status::ERR_BAD_VALUE)?;
        if name.is_empty() {
            return Err(text_status::ERR_BAD_VALUE);
        }
        names.push(name.to_owned());
        rest = if end == rest.len() { &[] } else { &rest[end + 1..] };
    }
    if names.len() != expected_count {
        return Err(text_status::ERR_BAD_VALUE);
    }
    Ok(names)
}

/// Builds the pinned `Font` descriptor from the ABI pieces.
fn build_font(
    family: &str,
    weight: FontWeight,
    style: FontStyle,
    features: &[GpuiGoTextFeature],
    fallbacks: &[String],
) -> Font {
    Font {
        family: family.into(),
        features: FontFeatures(std::sync::Arc::new(
            features.iter().map(feature_pair).collect::<Vec<_>>(),
        )),
        fallbacks: (!fallbacks.is_empty())
            .then(|| FontFallbacks::from_fonts(fallbacks.to_vec())),
        weight,
        style,
    }
}

/// Validates the shape request's run table against the text and builds the
/// pinned `TextRun` list (the descriptor form `shape_text` receives).
fn build_runs(
    text: &str,
    run_records: &[GpuiGoTextRunRecord],
    features: &[GpuiGoTextFeature],
    variations: &[GpuiGoTextVariation],
    strings: &[u8],
) -> Result<Vec<TextRun>, i32> {
    let mut runs = Vec::with_capacity(run_records.len());
    let mut covered = 0usize;
    for record in run_records {
        let family_len = record.family_len as usize;
        let family_offset = record.family_offset as usize;
        if family_len == 0
            || family_offset.checked_add(family_len).map_or(true, |end| end > strings.len())
        {
            return Err(text_status::ERR_BAD_VALUE);
        }
        let family =
            std::str::from_utf8(&strings[family_offset..family_offset + family_len])
                .map_err(|_| text_status::ERR_BAD_VALUE)?;
        if family.is_empty() {
            return Err(text_status::ERR_BAD_VALUE);
        }

        let fallbacks_len = record.fallbacks_len as usize;
        let fallbacks_offset = record.fallbacks_offset as usize;
        if fallbacks_len > 0
            && fallbacks_offset
                .checked_add(fallbacks_len)
                .map_or(true, |end| end > strings.len())
        {
            return Err(text_status::ERR_BAD_VALUE);
        }
        let fallback_region = if fallbacks_len == 0 {
            &[][..]
        } else {
            &strings[fallbacks_offset..fallbacks_offset + fallbacks_len]
        };
        // Count without materializing yet: the count must match the
        // NUL-separated segments.
        let fallback_count = fallback_region
            .iter()
            .filter(|&&b| b == 0)
            .count()
            + usize::from(
                !fallback_region.is_empty()
                    && *fallback_region.last().expect("checked non-empty") != 0,
            );
        if fallbacks_len > 0 && fallback_count == 0 {
            return Err(text_status::ERR_BAD_VALUE);
        }
        let fallbacks = split_names(fallback_region, fallback_count)?;

        let feature_offset = record.feature_offset as usize;
        let feature_count = record.feature_count as usize;
        if feature_count > 0
            && feature_offset
                .checked_add(feature_count)
                .map_or(true, |end| end > features.len())
        {
            return Err(text_status::ERR_BAD_VALUE);
        }
        let run_features = &features[feature_offset..feature_offset + feature_count];
        for feature in run_features {
            check_feature(feature)?;
        }

        // rev 15: the browser text fields. The tags use the pinned
        // Parley enumerations where Parley defines them (word-break,
        // overflow-wrap) and the CSS keywords otherwise (text-wrap);
        // the values are bounds-checked here and stored with the
        // shaping handle.
        if record.word_break > 2 || record.overflow_wrap > 2 || record.text_wrap > 4 {
            return Err(text_status::ERR_BAD_VALUE);
        }
        if record.justify > 1 {
            return Err(text_status::ERR_BAD_VALUE);
        }
        let locale_len = record.locale_len as usize;
        let locale_offset = record.locale_offset as usize;
        if locale_len > 0
            && locale_offset
                .checked_add(locale_len)
                .map_or(true, |end| end > strings.len())
        {
            return Err(text_status::ERR_BAD_VALUE);
        }
        if locale_len > 0 {
            std::str::from_utf8(&strings[locale_offset..locale_offset + locale_len])
                .map_err(|_| text_status::ERR_BAD_VALUE)?;
        }
        match record.font_width_present {
            0 => {}
            1 => {
                let width = finite_f32(record.font_width_bits)?;
                if width < 0.0 {
                    return Err(text_status::ERR_BAD_VALUE);
                }
            }
            _ => return Err(text_status::ERR_BAD_VALUE),
        }
        match record.opsz_present {
            0 => {}
            1 => {
                let opsz = finite_f32(record.opsz_bits)?;
                if opsz < 0.0 {
                    return Err(text_status::ERR_BAD_VALUE);
                }
            }
            _ => return Err(text_status::ERR_BAD_VALUE),
        }
        let variation_offset = record.variation_offset as usize;
        let variation_count = record.variation_count as usize;
        if variation_count > 0
            && variation_offset
                .checked_add(variation_count)
                .map_or(true, |end| end > variations.len())
        {
            return Err(text_status::ERR_BAD_VALUE);
        }
        for variation in &variations[variation_offset..variation_offset + variation_count] {
            check_variation(variation)?;
        }

        let len = record.len as usize;
        covered = covered
            .checked_add(len)
            .ok_or(text_status::ERR_BAD_VALUE)?;
        if covered > text.len() || !text.is_char_boundary(covered) {
            return Err(text_status::ERR_BAD_VALUE);
        }

        let letter_spacing = match record.letter_spacing_present {
            0 => None,
            1 => Some(px(finite_f32(record.letter_spacing_bits)?)),
            _ => return Err(text_status::ERR_BAD_VALUE),
        };

        let mut run = TextRun {
            len,
            font: build_font(
                family,
                weight_from_bits(record.weight_bits)?,
                style_from_u32(record.style)?,
                run_features,
                &fallbacks,
            ),
            ..TextRun::default()
        };
        run.letter_spacing = letter_spacing;
        runs.push(run);
    }
    if covered != text.len() {
        // Runs must cover the complete string (the shape_text contract).
        return Err(text_status::ERR_BAD_VALUE);
    }
    Ok(runs)
}

/// Validates a byte index for caret/cluster/selection queries.
fn check_byte_index(text: &str, index: u32) -> Result<usize, i32> {
    let index = index as usize;
    if index > text.len() || !text.is_char_boundary(index) {
        return Err(text_status::ERR_BAD_VALUE);
    }
    Ok(index)
}

/// Resolves the slot for a shaping handle, or a typed error.
fn with_shaping<T>(
    state: &TextServiceState,
    handle: u64,
    read: impl FnOnce(&ShapingState) -> Result<T, i32>,
) -> Result<T, i32> {
    let (slot_index, slot_generation) = shaping_handle_parts(handle);
    if slot_index >= state.slots.len() as usize {
        return Err(text_status::ERR_BAD_HANDLE);
    }
    let slot = &state.slots[slot_index];
    if !slot.occupied || slot.slot_generation != slot_generation {
        return Err(text_status::ERR_STALE_HANDLE);
    }
    let shaping = slot.shaping.as_ref().ok_or(text_status::ERR_STALE_HANDLE)?;
    read(shaping)
}

/// The pinned baseline derivation from `line.rs::paint_visual_text`:
/// `padding_top = (line_height - ascent - descent) / 2`, baseline =
/// `padding_top + ascent` (relative to the row's top edge).
fn line_baseline(line_height: Pixels, ascent: Pixels, descent: Pixels) -> Pixels {
    let padding_top = (line_height - ascent - descent) / 2.;
    padding_top + ascent
}

// ---------------------------------------------------------------------------
// Export bodies
// ---------------------------------------------------------------------------

fn text_font_names_body(
    buf: *mut u8,
    capacity: u32,
    out_count: *mut u32,
    out_needed: *mut u32,
) -> i32 {
    if out_count.is_null() || out_needed.is_null() {
        return text_status::ERR_NULL_ARG;
    }
    let mut state = lock_global();
    let stack = ensure_stack(&mut state);
    // The pinned all_font_names: catalog family names plus ".SystemUIFont",
    // sorted and deduplicated.
    let names = stack.system.all_font_names();
    let mut needed = 0usize;
    for name in &names {
        let Some(total) = needed.checked_add(name.len() + 1) else {
            return text_status::ERR_BAD_VALUE;
        };
        needed = total;
    }
    if needed > MAX_FONT_NAMES_BYTES as usize {
        return text_status::ERR_BAD_VALUE;
    }
    unsafe {
        *out_count = names.len() as u32;
        *out_needed = needed as u32;
    }
    if needed == 0 {
        return text_status::OK;
    }
    if (needed as u32) > capacity {
        return text_status::ERR_CAPACITY;
    }
    if buf.is_null() {
        return text_status::ERR_NULL_ARG;
    }
    let mut cursor = buf;
    unsafe {
        for name in &names {
            std::ptr::copy_nonoverlapping(name.as_ptr(), cursor, name.len());
            cursor = cursor.add(name.len());
            *cursor = 0;
            cursor = cursor.add(1);
        }
    }
    text_status::OK
}

/// The weight the pinned resolution should see (gpui-perf ticket 05,
/// zterm ticket 13 and browser-core ticket 39, recorded deviations from
/// the pin). Fontique 0.11.1 (font.rs:97-103) asks for bold synthesis
/// whenever the requested weight is above the selected face's weight and
/// the face has no `wght` axis, so 500 on a family whose closest face is
/// the static Regular (400) rendered Regular through DirectWrite's bold
/// simulation, and 700 on a family with SemiBold (600) as its heaviest
/// face rendered SemiBold plus the simulation (a smear browsers never
/// draw). Chrome on Windows synthesizes bold only when the request is
/// at least 600 AND the matched face is not already 600 or heavier
/// (Chromium 0d4dcd4f, third_party/blink/renderer/platform/fonts/win/
/// font_cache_skia_win.cc:484-486 — compiled on Windows per
/// platform/BUILD.gn:1810-1813 — with kBoldThreshold = 600 from
/// platform/fonts/font_selection_types.h:183 and SkTypeface::isBold()
/// meaning weight >= 600, skia src/core/SkTypeface.cpp:491-493). The
/// ">200 above the face" rule this service previously followed
/// (skia/font_cache_skia.cc:347-353) is the non-Windows path: it sits
/// inside `#if !BUILDFLAG(IS_WIN)` (font_cache_skia.cc:312) and is
/// compiled out on Windows, so 600 over 400 and 700 over 500 must
/// synthesize and 900 over 600 must not. When Fontique would synthesize
/// and Chrome would not, the request is replaced by the selected face's
/// own OS/2 weight, which selects the same face with no synthesis; the
/// replacement is verified (same face bytes, no embolden) and otherwise
/// dropped. Requests below 600 (CSS never synthesizes there), requests
/// whose matched face is already 600 or heavier, and faces with a `wght`
/// axis (where Fontique sets the axis instead of emboldening) are
/// unchanged. The oracle `ce-source` stays unmodified; this runs in the
/// native service before every resolve and every shaping run.
fn effective_weight(
    state: &mut TextServiceState,
    stack: &TextStack,
    font: &Font,
) -> FontWeight {
    let weight = font.weight;
    let style_tag = match font.style {
        FontStyle::Normal => 0u8,
        FontStyle::Italic => 1,
        FontStyle::Oblique => 2,
    };
    let fallbacks = font
        .fallbacks
        .as_ref()
        .map(|fallbacks| fallbacks.fallback_list().to_vec())
        .unwrap_or_default();
    let key: WeightKey = (font.family.to_string(), fallbacks, weight.0.to_bits(), style_tag);
    if let Some(bits) = state.weight_fixups.get(&key) {
        return FontWeight(f32::from_bits(*bits));
    }
    let probe_font = |weight: FontWeight| Font {
        weight,
        features: FontFeatures::default(),
        ..font.clone()
    };
    let fixed = (|| {
        let requested = stack.parley.font_id(&probe_font(weight)).ok()?;
        let probe = crate::glyph::probe_face(&stack.parley, requested)?;
        if !probe.embolden {
            return None;
        }
        let face_weight = f32::from(probe.weight_class?);
        if !(face_weight < weight.0) {
            return None;
        }
        // Synthesis stays only where Chrome on Windows would draw it
        // too: the request is at least 600 (kBoldThreshold) and the
        // matched face is below 600 (SkTypeface::isBold()).
        if weight.0 >= 600.0 && face_weight < 600.0 {
            return None;
        }
        let candidate = FontWeight(face_weight);
        let replaced = stack.parley.font_id(&probe_font(candidate)).ok()?;
        let check = crate::glyph::probe_face(&stack.parley, replaced)?;
        (!check.embolden && check.same_face(&probe)).then_some(candidate)
    })();
    let effective = fixed.unwrap_or(weight);
    state.weight_fixups.insert(key, effective.0.to_bits());
    effective
}

/// Shared body of `text_font_resolve`.
fn resolve_font_descriptor(
    state: &mut TextServiceState,
    stack: &TextStack,
    request: &GpuiGoTextFontRequest,
    family_bytes: &[u8],
    features: &[GpuiGoTextFeature],
    fallbacks_bytes: &[u8],
) -> Result<FontId, i32> {
    if request.reserved != [0u32; 3] {
        return Err(text_status::ERR_BAD_VALUE);
    }
    let family = std::str::from_utf8(family_bytes).map_err(|_| text_status::ERR_BAD_VALUE)?;
    if family.is_empty() {
        return Err(text_status::ERR_BAD_VALUE);
    }
    for feature in features {
        check_feature(feature)?;
    }
    let fallbacks = split_names(fallbacks_bytes, request.fallback_count as usize)?;
    let mut font = build_font(
        family,
        weight_from_bits(request.weight_bits)?,
        style_from_u32(request.style)?,
        features,
        &fallbacks,
    );
    font.weight = effective_weight(state, stack, &font);
    // The pinned resolve_canonical_font (typed Result; the gpui wrapper's
    // resolve_font panics here, which the ABI must not).
    let font_id = stack
        .parley
        .font_id(&font)
        .map_err(|_| text_status::ERR_FONT_UNRESOLVED)?;
    register_font(state, stack, font_id);
    Ok(font_id)
}

fn text_font_resolve_body(
    request: *const GpuiGoTextFontRequest,
    family: *const u8,
    family_len: u32,
    features: *const GpuiGoTextFeature,
    feature_count: u32,
    fallbacks: *const u8,
    fallbacks_len: u32,
    out: *mut GpuiGoTextFontRecord,
) -> i32 {
    if request.is_null() || out.is_null() {
        return text_status::ERR_NULL_ARG;
    }
    if feature_count > MAX_FEATURES {
        return text_status::ERR_BAD_VALUE;
    }
    let request: &GpuiGoTextFontRequest = unsafe { &*request };
    unsafe {
        let family_bytes = match borrowed_slice(family, family_len) {
            Ok(bytes) => bytes,
            Err(code) => return code,
        };
        let features = match borrowed_records(features, feature_count) {
            Ok(records) => records,
            Err(code) => return code,
        };
        let fallbacks_bytes = match borrowed_slice(fallbacks, fallbacks_len) {
            Ok(bytes) => bytes,
            Err(code) => return code,
        };
        let mut state = lock_global();
        let stack = ensure_stack(&mut state);
        let font_id = match resolve_font_descriptor(
            &mut state,
            &stack,
            request,
            family_bytes,
            features,
            fallbacks_bytes,
        ) {
            Ok(id) => id,
            Err(code) => return code,
        };
        let generation = state
            .fonts
            .get(&(font_id.0 as u64))
            .copied()
            .unwrap_or_default();
        // The family-presence diagnostic: 1 when the primary family is in
        // the catalog, 0 when the fallback chain answered (the silent
        // fallback stays behaviorally pinned — this only reports it).
        let family_found = u32::from(family_present(
            &mut state,
            &stack,
            std::str::from_utf8(family_bytes).unwrap_or(""),
        ));
        let out: &mut GpuiGoTextFontRecord = &mut *out;
        out.font_id = font_id.0 as u64;
        out.generation = generation;
        out.weight_bits = request.weight_bits;
        out.style = request.style;
        out.feature_count = request.feature_count;
        out.fallback_count = request.fallback_count;
        out.family_found = family_found;
        out.record_size = std::mem::size_of::<GpuiGoTextFontRecord>() as u32;
        text_status::OK
    }
}

/// Shared body of `text_font_add`: registers in-memory font bytes with
/// the catalog through the pinned `PlatformTextSystem::add_fonts` path
/// (`fontique::Blob` → `register_blobs`, exactly the code the pinned
/// `TextSystem::add_fonts` runs; family names come from each font's
/// embedded name table). Registration is visible to every later
/// resolve/shape/rasterize call on the same stack (one Fontique catalog
/// for the whole process). The bytes are copied into service-owned state
/// before registration, so no caller pointer is retained.
///
/// One blob per call: the pinned `add_fonts` takes a `Vec` of sources and
/// registers them as one batch; the ABI registers one blob per call, so
/// N calls produce N registration batches (N generation bumps). The
/// observable outcomes — FontIds, family names, resolution — are
/// unaffected.
fn add_font_body(data: *const u8, data_len: u32, out_generation: *mut u64) -> i32 {
    if out_generation.is_null() {
        return text_status::ERR_NULL_ARG;
    }
    if data_len == 0 || data_len > MAX_FONT_BYTES {
        return text_status::ERR_BAD_VALUE;
    }
    if data.is_null() {
        return text_status::ERR_NULL_ARG;
    }
    let bytes = unsafe { std::slice::from_raw_parts(data, data_len as usize) };
    let mut state = lock_global();
    let stack = ensure_stack(&mut state);
    // The pinned registration: any failure (no supported face in the data,
    // Skrifa parse failure) is the typed font-data error, never a panic at
    // the ABI.
    match stack.parley.add_fonts(vec![std::borrow::Cow::Owned(bytes.to_vec())]) {
        Ok(()) => {
            // The generation bumps with every successful registration
            // batch; report it so a caller can observe the registration
            // happened (the catalog generation the next resolve reports).
            let generation = stack.parley.font_generation();
            // The family-name cache is keyed by generation, so it is
            // already invalidated by construction; drop it eagerly to free
            // the stale set.
            state.family_names = None;
            state.weight_fixups.clear();
            unsafe { *out_generation = generation };
            text_status::OK
        }
        Err(_) => text_status::ERR_FONT_DATA,
    }
}

fn text_font_metrics_body(
    font_id: u64,
    out: *mut GpuiGoTextFontMetricsRecord,
) -> i32 {
    if out.is_null() {
        return text_status::ERR_NULL_ARG;
    }
    let mut state = lock_global();
    if font_id & FONT_ID_CANONICAL_BIT == 0 || !state.fonts.contains_key(&font_id) {
        return text_status::ERR_BAD_HANDLE;
    }
    let stack = ensure_stack(&mut state);
    // Registered ids are interned in the pinned store by construction, so
    // the pinned font_metrics cannot miss (the catch_unwind boundary stays
    // defense in depth for the expect).
    let metrics = stack.parley.font_metrics(FontId(font_id as usize));
    let out: &mut GpuiGoTextFontMetricsRecord = unsafe { &mut *out };
    out.units_per_em = metrics.units_per_em;
    out.ascent_bits = metrics.ascent.to_bits();
    out.descent_bits = metrics.descent.to_bits();
    out.line_gap_bits = metrics.line_gap.to_bits();
    out.underline_position_bits = metrics.underline_position.to_bits();
    out.underline_thickness_bits = metrics.underline_thickness.to_bits();
    out.cap_height_bits = metrics.cap_height.to_bits();
    out.x_height_bits = metrics.x_height.to_bits();
    out.bbox_x_bits = metrics.bounding_box.origin.x.to_bits();
    out.bbox_y_bits = metrics.bounding_box.origin.y.to_bits();
    out.bbox_w_bits = metrics.bounding_box.size.width.to_bits();
    out.bbox_h_bits = metrics.bounding_box.size.height.to_bits();
    out.record_size = std::mem::size_of::<GpuiGoTextFontMetricsRecord>() as u32;
    text_status::OK
}

/// rev 15.1 (browser-core ticket 77): CSS `text-wrap: nowrap` drives
/// behaviour through the existing pinned request — the layout does not
/// soft-wrap when any run carries the tag, exactly like passing no wrap
/// width. This is the one rev-15 browser text field the pinned public
/// request can express (wrap_width: Option); see TEXT_ABI.md for the
/// fields that stay blocked behind the pinned adapter's private Parley
/// state. Document-level, matching CSS's block-level text-wrap: the
/// pinned `TextLayoutRequest` carries one wrap constraint for the whole
/// document, so a per-run wrap policy is not expressible without the
/// same adapter change that blocks word-break.
fn nowrap_requested(run_records: &[GpuiGoTextRunRecord]) -> bool {
    run_records
        .iter()
        .any(|record| record.text_wrap == TEXT_WRAP_NOWRAP)
}

/// The shaping core shared by `text_shape` and `text_layout`: calls the
/// pinned `layout_text` and registers every resolved fragment font.
fn shape_document(
    state: &mut TextServiceState,
    stack: &TextStack,
    text: &str,
    runs: &[TextRun],
    font_size: Pixels,
    wrap_width: Option<Pixels>,
    line_clamp: Option<usize>,
) -> Result<Arc<LineLayout>, i32> {
    // gpui-perf ticket 05 / zterm ticket 13: the same weight replacement as
    // the resolve path, so per-run shaping synthesizes bold only where a
    // browser would.
    let mut adjusted: Option<Vec<TextRun>> = None;
    for (index, run) in runs.iter().enumerate() {
        let weight = effective_weight(state, stack, &run.font);
        if weight != run.font.weight {
            let runs = adjusted.get_or_insert_with(|| runs.to_vec());
            runs[index].font.weight = weight;
        }
    }
    let runs: &[TextRun] = adjusted.as_deref().unwrap_or(runs);
    // Pre-resolve every run's font so an unresolvable family is a typed
    // error (the pinned layout_text expects validated input and panics on
    // resolve failures). This mirrors resolve_canonical_font exactly.
    for run in runs {
        let _ = stack
            .parley
            .font_id(&run.font)
            .map_err(|_| text_status::ERR_FONT_UNRESOLVED)?;
    }
    // The pinned shaping call: the exact entry shape_text/layout_line make.
    let layout = stack.parley.layout_text(TextLayoutRequest {
        text,
        font_size,
        runs,
        wrap_width,
        line_clamp,
    });
    // Register every resolved fragment font id (shaping may select
    // fallback faces the caller never resolved).
    for fragment in &layout.paint_fragments {
        register_font(state, stack, fragment.font_id);
    }
    Ok(Arc::new(layout))
}

#[allow(clippy::too_many_arguments)]
fn text_shape_body(
    text: *const u8,
    text_len: u32,
    run_records: *const GpuiGoTextRunRecord,
    run_count: u32,
    features: *const GpuiGoTextFeature,
    feature_count: u32,
    variations: *const GpuiGoTextVariation,
    variation_count: u32,
    strings: *const u8,
    strings_len: u32,
    font_size_bits: u32,
    wrap_width: u32,
    wrap_present: u32,
    line_clamp: u32,
    clamp_present: u32,
    out_handle: *mut u64,
) -> i32 {
    if out_handle.is_null() {
        return text_status::ERR_NULL_ARG;
    }
    if text_len > MAX_TEXT_BYTES || run_count > MAX_RUNS || feature_count > MAX_FEATURES {
        return text_status::ERR_BAD_VALUE;
    }
    if variation_count > MAX_VARIATIONS {
        return text_status::ERR_BAD_VALUE;
    }
    if strings_len > MAX_STRINGS_BYTES {
        return text_status::ERR_BAD_VALUE;
    }
    let font_size = match finite_f32(font_size_bits) {
        Ok(value) => px(value),
        Err(code) => return code,
    };
    let wrap_width = match (wrap_present, wrap_width) {
        (0, _) => None,
        (1, bits) => match finite_f32(bits) {
            // Negative wrap widths are not valid constraints.
            Ok(value) if value >= 0.0 => Some(px(value)),
            _ => return text_status::ERR_BAD_VALUE,
        },
        _ => return text_status::ERR_BAD_VALUE,
    };
    let line_clamp = match (clamp_present, line_clamp) {
        (0, _) => None,
        (1, clamp) if clamp >= 1 => Some(clamp as usize),
        _ => return text_status::ERR_BAD_VALUE,
    };
    unsafe {
        let text_bytes = match borrowed_slice(text, text_len) {
            Ok(bytes) => bytes,
            Err(code) => return code,
        };
        let text = match std::str::from_utf8(text_bytes) {
            Ok(text) => text,
            Err(_) => return text_status::ERR_BAD_VALUE,
        };
        let run_records = match borrowed_records(run_records, run_count) {
            Ok(records) => records,
            Err(code) => return code,
        };
        let features = match borrowed_records(features, feature_count) {
            Ok(records) => records,
            Err(code) => return code,
        };
        let variations = match borrowed_records(variations, variation_count) {
            Ok(records) => records,
            Err(code) => return code,
        };
        let strings = match borrowed_slice(strings, strings_len) {
            Ok(bytes) => bytes,
            Err(code) => return code,
        };
        // Build the pinned run list (validates coverage, boundaries,
        // features, the rev-15 browser text fields and fallback
        // packing).
        let runs = match build_runs(text, run_records, features, variations, strings) {
            Ok(runs) => runs,
            Err(code) => return code,
        };

        let mut state = lock_global();
        let stack = ensure_stack(&mut state);
        // rev 15.1: text-wrap: nowrap suppresses the soft-wrap constraint
        // (CSS text-wrap is a wrap policy on the whole document; the
        // layout summary reports the ACTIVE constraint, so the echo says
        // no wrap while the run records still carry the nowrap tag).
        let wrap_width = wrap_width.filter(|_| !nowrap_requested(run_records));
        let layout = match shape_document(
            &mut state,
            &stack,
            text,
            &runs,
            font_size,
            wrap_width,
            line_clamp,
        ) {
            Ok(layout) => layout,
            Err(code) => return code,
        };
        let font_generation = stack.parley.font_generation();

        // Capacity: bound live shaping handles before publishing anything.
        let slot_index = match state.slots.iter().position(|slot| !slot.occupied) {
            Some(index) => index,
            None if state.slots.len() < MAX_SHAPING_SLOTS as usize => {
                state.slots.push(ShapingSlot::vacant());
                state.slots.len() - 1
            }
            None => return text_status::ERR_SHAPING_LIMIT,
        };
        let slot_generation = match state.slots[slot_index].slot_generation.checked_add(1) {
            Some(generation) if generation <= SHAPING_GENERATION_MAX => generation,
            _ => return text_status::ERR_BAD_VALUE,
        };
        state.slots[slot_index] = ShapingSlot {
            slot_generation,
            occupied: true,
            shaping: Some(ShapingState {
                text: text.to_owned(),
                runs,
                run_records: run_records.to_vec(),
                font_size,
                wrap_width,
                line_clamp,
                layout,
                font_generation,
            }),
        };
        *out_handle = make_shaping_handle(slot_index, slot_generation);
        text_status::OK
    }
}

fn text_layout_body(
    handle: u64,
    wrap_width: u32,
    wrap_present: u32,
    line_clamp: u32,
    clamp_present: u32,
) -> i32 {
    let wrap_width = match (wrap_present, wrap_width) {
        (0, _) => None,
        (1, bits) => match finite_f32(bits) {
            Ok(value) if value >= 0.0 => Some(px(value)),
            _ => return text_status::ERR_BAD_VALUE,
        },
        _ => return text_status::ERR_BAD_VALUE,
    };
    let line_clamp = match (clamp_present, line_clamp) {
        (0, _) => None,
        (1, clamp) if clamp >= 1 => Some(clamp as usize),
        _ => return text_status::ERR_BAD_VALUE,
    };
    let mut state = lock_global();
    let (slot_index, slot_generation) = shaping_handle_parts(handle);
    if slot_index >= state.slots.len() {
        return text_status::ERR_BAD_HANDLE;
    }
    let slot = &state.slots[slot_index];
    if !slot.occupied || slot.slot_generation != slot_generation {
        return text_status::ERR_STALE_HANDLE;
    }
    let shaping = match slot.shaping.as_ref() {
        Some(shaping) => shaping,
        None => return text_status::ERR_STALE_HANDLE,
    };
    let text = shaping.text.clone();
    let runs = shaping.runs.clone();
    let run_records = shaping.run_records.clone();
    let font_size = shaping.font_size;
    let stack = ensure_stack(&mut state);
    // rev 15.1: nowrap suppresses any re-shaped wrap constraint too
    // (text_layout re-shapes the SAME runs; their text-wrap tag still
    // governs).
    let wrap_width = wrap_width.filter(|_| !nowrap_requested(&run_records));
    let layout = match shape_document(
        &mut state,
        &stack,
        &text,
        &runs,
        font_size,
        wrap_width,
        line_clamp,
    ) {
        Ok(layout) => layout,
        Err(code) => return code,
    };
    let font_generation = stack.parley.font_generation();
    let slot = &mut state.slots[slot_index];
    let shaping = match slot.shaping.as_mut() {
        Some(shaping) => shaping,
        None => return text_status::ERR_STALE_HANDLE,
    };
    shaping.wrap_width = wrap_width;
    shaping.line_clamp = line_clamp;
    shaping.layout = layout;
    shaping.font_generation = font_generation;
    text_status::OK
}

fn text_dispose_body(handle: u64) -> i32 {
    let (slot_index, slot_generation) = shaping_handle_parts(handle);
    let mut state = lock_global();
    if slot_index >= state.slots.len() {
        return text_status::ERR_BAD_HANDLE;
    }
    let slot = &mut state.slots[slot_index];
    if !slot.occupied || slot.slot_generation != slot_generation {
        return text_status::ERR_STALE_HANDLE;
    }
    // Drop the document while still holding the registry lock: no other
    // export can observe the slot mid-drop.
    slot.occupied = false;
    slot.shaping = None;
    text_status::OK
}

/// The `text_run_info` body (rev 15): echoes one stored run record.
fn text_run_info_body(handle: u64, run_index: u32, out: *mut GpuiGoTextRunRecord) -> i32 {
    if out.is_null() {
        return text_status::ERR_NULL_ARG;
    }
    let state = lock_global();
    let result = with_shaping(&state, handle, |shaping| {
        let record = shaping
            .run_records
            .get(run_index as usize)
            .copied()
            .ok_or(text_status::ERR_BAD_VALUE)?;
        Ok(record)
    });
    match result {
        Ok(record) => {
            // SAFETY: the caller owns the out record.
            unsafe { *out = record };
            text_status::OK
        }
        Err(code) => code,
    }
}

fn text_layout_info_body(handle: u64, out: *mut GpuiGoTextLayoutRecord) -> i32 {
    if out.is_null() {
        return text_status::ERR_NULL_ARG;
    }
    let state = lock_global();
    let result = with_shaping(&state, handle, |shaping| {
        let layout: &LineLayout = &shaping.layout;
        Ok(GpuiGoTextLayoutRecord {
            text_len: shaping.text.len() as u32,
            font_size_bits: f32::from(shaping.font_size).to_bits(),
            wrap_present: u32::from(shaping.wrap_width.is_some()),
            wrap_bits: shaping
                .wrap_width
                .map_or(0.0f32, |width| f32::from(width))
                .to_bits(),
            clamp_present: u32::from(shaping.line_clamp.is_some()),
            line_clamp: shaping.line_clamp.map_or(0, |clamp| clamp as u32),
            line_count: layout.visual_lines.len() as u32,
            fragment_count: layout.paint_fragments.len() as u32,
            glyph_count: layout
                .paint_fragments
                .iter()
                .map(|fragment| fragment.glyphs.len())
                .sum::<usize>() as u32,
            width_bits: f32::from(layout.width).to_bits(),
            ascent_bits: f32::from(layout.ascent).to_bits(),
            descent_bits: f32::from(layout.descent).to_bits(),
            font_generation: shaping.font_generation as u32,
            record_size: std::mem::size_of::<GpuiGoTextLayoutRecord>() as u32,
        })
    });
    match result {
        Ok(record) => {
            unsafe { *out = record };
            text_status::OK
        }
        Err(code) => code,
    }
}

fn text_line_count_body(handle: u64, out: *mut u32) -> i32 {
    if out.is_null() {
        return text_status::ERR_NULL_ARG;
    }
    let state = lock_global();
    let result = with_shaping(&state, handle, |shaping| {
        Ok(shaping.layout.visual_lines.len() as u32)
    });
    match result {
        Ok(count) => {
            unsafe { *out = count };
            text_status::OK
        }
        Err(code) => code,
    }
}

fn text_line_body(
    handle: u64,
    index: u32,
    line_height_bits: u32,
    out: *mut GpuiGoTextLineRecord,
) -> i32 {
    if out.is_null() {
        return text_status::ERR_NULL_ARG;
    }
    let line_height = match finite_f32(line_height_bits) {
        Ok(value) => px(value),
        Err(code) => return code,
    };
    let state = lock_global();
    let result = with_shaping(&state, handle, |shaping| {
        let layout: &LineLayout = &shaping.layout;
        let index = index as usize;
        let line = layout.visual_lines.get(index).ok_or(text_status::ERR_BAD_VALUE)?;
        Ok(GpuiGoTextLineRecord {
            line_index: index as u32,
            text_start: line.text_range.start as u32,
            text_end: line.text_range.end as u32,
            fragment_start: line.fragment_range.start as u32,
            fragment_end: line.fragment_range.end as u32,
            advance_width_bits: f32::from(line.advance_width).to_bits(),
            line_height_bits: f32::from(line_height).to_bits(),
            baseline_bits: f32::from(line_baseline(line_height, layout.ascent, layout.descent))
                .to_bits(),
            record_size: std::mem::size_of::<GpuiGoTextLineRecord>() as u32,
        })
    });
    match result {
        Ok(record) => {
            unsafe { *out = record };
            text_status::OK
        }
        Err(code) => code,
    }
}

/// Caret affinity from the ABI enumerant.
fn affinity_from_u32(affinity: u32) -> Result<CaretAffinity, i32> {
    match affinity {
        0 => Ok(CaretAffinity::Downstream),
        1 => Ok(CaretAffinity::Upstream),
        _ => Err(text_status::ERR_BAD_VALUE),
    }
}

fn text_caret_body(
    handle: u64,
    index: u32,
    affinity: u32,
    line_height_bits: u32,
    out: *mut GpuiGoTextCaretRecord,
) -> i32 {
    if out.is_null() {
        return text_status::ERR_NULL_ARG;
    }
    let line_height = match finite_f32(line_height_bits) {
        Ok(value) => px(value),
        Err(code) => return code,
    };
    let affinity = match affinity_from_u32(affinity) {
        Ok(affinity) => affinity,
        Err(code) => return code,
    };
    let state = lock_global();
    let result = with_shaping(&state, handle, |shaping| {
        let index = check_byte_index(&shaping.text, index)?;
        let caret = CaretPosition { index, affinity };
        let layout: &LineLayout = &shaping.layout;
        let bounds: Option<Bounds<Pixels>> =
            layout.platform_layout.caret_bounds(caret, line_height);
        let before = layout.platform_layout.logical_cluster_before(caret);
        let after = layout.platform_layout.logical_cluster_after(caret);
        let mut record = GpuiGoTextCaretRecord {
            index: index as u32,
            affinity: u32::from(affinity == CaretAffinity::Upstream),
            present: 0,
            x_bits: 0.0f32.to_bits(),
            y_bits: 0.0f32.to_bits(),
            w_bits: 0.0f32.to_bits(),
            h_bits: 0.0f32.to_bits(),
            cluster_before_present: 0,
            cluster_before_start: 0,
            cluster_before_end: 0,
            cluster_after_present: 0,
            cluster_after_start: 0,
            cluster_after_end: 0,
            record_size: std::mem::size_of::<GpuiGoTextCaretRecord>() as u32,
        };
        if let Some(bounds) = bounds {
            record.present = 1;
            record.x_bits = f32::from(bounds.origin.x).to_bits();
            record.y_bits = f32::from(bounds.origin.y).to_bits();
            record.w_bits = f32::from(bounds.size.width).to_bits();
            record.h_bits = f32::from(bounds.size.height).to_bits();
        }
        if let Some(range) = before {
            record.cluster_before_present = 1;
            record.cluster_before_start = range.start as u32;
            record.cluster_before_end = range.end as u32;
        }
        if let Some(range) = after {
            record.cluster_after_present = 1;
            record.cluster_after_start = range.start as u32;
            record.cluster_after_end = range.end as u32;
        }
        Ok(record)
    });
    match result {
        Ok(record) => {
            unsafe { *out = record };
            text_status::OK
        }
        Err(code) => code,
    }
}

fn text_hit_test_body(
    handle: u64,
    x_bits: u32,
    y_bits: u32,
    line_height_bits: u32,
    out: *mut GpuiGoTextHitRecord,
) -> i32 {
    if out.is_null() {
        return text_status::ERR_NULL_ARG;
    }
    let x = match finite_f32(x_bits) {
        Ok(value) => value,
        Err(code) => return code,
    };
    let y = match finite_f32(y_bits) {
        Ok(value) => value,
        Err(code) => return code,
    };
    let point = Point { x: px(x), y: px(y) };
    let line_height = match finite_f32(line_height_bits) {
        Ok(value) => px(value),
        Err(code) => return code,
    };
    let state = lock_global();
    let result = with_shaping(&state, handle, |shaping| {
        let layout: &LineLayout = &shaping.layout;
        // The pinned caret_from_pixel_point: Ok = inside a visual row,
        // Err = the edge caret for an outside point.
        let caret = layout
            .platform_layout
            .caret_from_pixel_point(point, line_height);
        let (caret, inside) = match caret {
            Ok(caret) => (caret, 1),
            Err(caret) => (caret, 0),
        };
        Ok(GpuiGoTextHitRecord {
            index: caret.index as u32,
            affinity: u32::from(caret.affinity == CaretAffinity::Upstream),
            inside,
            record_size: std::mem::size_of::<GpuiGoTextHitRecord>() as u32,
        })
    });
    match result {
        Ok(record) => {
            unsafe { *out = record };
            text_status::OK
        }
        Err(code) => code,
    }
}

fn text_selection_rects_body(
    handle: u64,
    start: u32,
    end: u32,
    line_height_bits: u32,
    rects: *mut GpuiGoTextSelectionRecord,
    capacity: u32,
    out_needed: *mut u32,
) -> i32 {
    if out_needed.is_null() {
        return text_status::ERR_NULL_ARG;
    }
    if start > end {
        return text_status::ERR_BAD_VALUE;
    }
    let line_height = match finite_f32(line_height_bits) {
        Ok(value) => px(value),
        Err(code) => return code,
    };
    let state = lock_global();
    let result = with_shaping(&state, handle, |shaping| {
        let start = check_byte_index(&shaping.text, start)?;
        let end = check_byte_index(&shaping.text, end)?;
        let layout: &LineLayout = &shaping.layout;
        // The pinned selection_bounds: visual-order rectangles for the
        // selected clusters (empty input range → no rectangles).
        let bounds = layout
            .platform_layout
            .selection_bounds(start..end, line_height);
        Ok(bounds)
    });
    let bounds = match result {
        Ok(bounds) => bounds,
        Err(code) => {
            unsafe { *out_needed = 0 };
            return code;
        }
    };
    let needed = bounds.len();
    unsafe { *out_needed = needed as u32 };
    if needed == 0 {
        return text_status::OK;
    }
    if needed as u32 > capacity {
        return text_status::ERR_CAPACITY;
    }
    if rects.is_null() {
        return text_status::ERR_NULL_ARG;
    }
    unsafe {
        let records: &mut [GpuiGoTextSelectionRecord] =
            std::slice::from_raw_parts_mut(rects, needed);
        for (record, bounds) in records.iter_mut().zip(bounds) {
            record.x_bits = f32::from(bounds.origin.x).to_bits();
            record.y_bits = f32::from(bounds.origin.y).to_bits();
            record.w_bits = f32::from(bounds.size.width).to_bits();
            record.h_bits = f32::from(bounds.size.height).to_bits();
            record.record_size = std::mem::size_of::<GpuiGoTextSelectionRecord>() as u32;
        }
    }
    text_status::OK
}

fn text_cluster_body(
    handle: u64,
    index: u32,
    affinity: u32,
    side: u32,
    out: *mut GpuiGoTextClusterRecord,
) -> i32 {
    if out.is_null() {
        return text_status::ERR_NULL_ARG;
    }
    let affinity = match affinity_from_u32(affinity) {
        Ok(affinity) => affinity,
        Err(code) => return code,
    };
    // 0 = the cluster before the caret, 1 = the cluster after.
    let after = match side {
        0 => false,
        1 => true,
        _ => return text_status::ERR_BAD_VALUE,
    };
    let state = lock_global();
    let result = with_shaping(&state, handle, |shaping| {
        let index = check_byte_index(&shaping.text, index)?;
        let caret = CaretPosition { index, affinity };
        let layout: &LineLayout = &shaping.layout;
        let range = if after {
            layout.platform_layout.logical_cluster_after(caret)
        } else {
            layout.platform_layout.logical_cluster_before(caret)
        };
        let mut record = GpuiGoTextClusterRecord {
            present: 0,
            start: 0,
            end: 0,
            record_size: std::mem::size_of::<GpuiGoTextClusterRecord>() as u32,
        };
        if let Some(range) = range {
            record.present = 1;
            record.start = range.start as u32;
            record.end = range.end as u32;
        }
        Ok(record)
    });
    match result {
        Ok(record) => {
            unsafe { *out = record };
            text_status::OK
        }
        Err(code) => code,
    }
}

fn text_fragments_body(
    handle: u64,
    records: *mut GpuiGoTextFragmentRecord,
    capacity: u32,
    out_needed: *mut u32,
) -> i32 {
    if out_needed.is_null() {
        return text_status::ERR_NULL_ARG;
    }
    let state = lock_global();
    let result = with_shaping(&state, handle, |shaping| {
        let layout: &LineLayout = &shaping.layout;
        let mut dump = Vec::with_capacity(layout.paint_fragments.len());
        let mut glyph_start = 0u32;
        for fragment in &layout.paint_fragments {
            dump.push(GpuiGoTextFragmentRecord {
                font_id: fragment.font_id.0 as u64,
                font_size_bits: f32::from(fragment.font_size).to_bits(),
                x_start_bits: f32::from(fragment.x_range.start).to_bits(),
                x_end_bits: f32::from(fragment.x_range.end).to_bits(),
                glyph_start,
                glyph_count: fragment.glyphs.len() as u32,
                record_size: std::mem::size_of::<GpuiGoTextFragmentRecord>() as u32,
            });
            glyph_start += fragment.glyphs.len() as u32;
        }
        Ok(dump)
    });
    let dump = match result {
        Ok(dump) => dump,
        Err(code) => {
            unsafe { *out_needed = 0 };
            return code;
        }
    };
    let needed = dump.len();
    unsafe { *out_needed = needed as u32 };
    if needed == 0 {
        return text_status::OK;
    }
    if needed as u32 > capacity {
        return text_status::ERR_CAPACITY;
    }
    if records.is_null() {
        return text_status::ERR_NULL_ARG;
    }
    unsafe {
        let out: &mut [GpuiGoTextFragmentRecord] =
            std::slice::from_raw_parts_mut(records, needed);
        out.copy_from_slice(&dump);
    }
    text_status::OK
}

fn text_glyphs_body(
    handle: u64,
    records: *mut GpuiGoTextGlyphRecord,
    capacity: u32,
    out_needed: *mut u32,
) -> i32 {
    if out_needed.is_null() {
        return text_status::ERR_NULL_ARG;
    }
    let state = lock_global();
    let result = with_shaping(&state, handle, |shaping| {
        let layout: &LineLayout = &shaping.layout;
        let total: usize = layout
            .paint_fragments
            .iter()
            .map(|fragment| fragment.glyphs.len())
            .sum();
        let mut dump = Vec::with_capacity(total);
        for fragment in &layout.paint_fragments {
            for glyph in &fragment.glyphs {
                dump.push(GpuiGoTextGlyphRecord {
                    glyph_id: glyph.id.0,
                    x_bits: f32::from(glyph.position.x).to_bits(),
                    y_bits: f32::from(glyph.position.y).to_bits(),
                    is_emoji: u32::from(glyph.is_emoji),
                    record_size: std::mem::size_of::<GpuiGoTextGlyphRecord>() as u32,
                });
            }
        }
        Ok(dump)
    });
    let dump = match result {
        Ok(dump) => dump,
        Err(code) => {
            unsafe { *out_needed = 0 };
            return code;
        }
    };
    let needed = dump.len();
    unsafe { *out_needed = needed as u32 };
    if needed == 0 {
        return text_status::OK;
    }
    if needed as u32 > capacity {
        return text_status::ERR_CAPACITY;
    }
    if records.is_null() {
        return text_status::ERR_NULL_ARG;
    }
    unsafe {
        let out: &mut [GpuiGoTextGlyphRecord] = std::slice::from_raw_parts_mut(records, needed);
        out.copy_from_slice(&dump);
    }
    text_status::OK
}

// ---------------------------------------------------------------------------
// Exported entries (the catch_unwind panic boundary)
// ---------------------------------------------------------------------------

/// Wraps a body with the panic boundary (the pattern every export follows).
macro_rules! text_entry {
    ($name:literal, $body:expr) => {{
        let outcome = catch_unwind(AssertUnwindSafe(|| $body));
        match outcome {
            Ok(code) => code,
            Err(payload) => {
                // Drop the payload; nothing is retained. The state mutex
                // recovers from poisoning on the next lock (no callback can
                // run under it, so no partial state survives).
                drop(payload);
                text_status::ERR_PANIC
            }
        }
    }};
}

unsafe extern "system" fn text_font_names(
    buf: *mut u8,
    capacity: u32,
    out_count: *mut u32,
    out_needed: *mut u32,
) -> i32 {
    text_entry!("text_font_names", text_font_names_body(buf, capacity, out_count, out_needed))
}

unsafe extern "system" fn text_font_resolve(
    request: *const GpuiGoTextFontRequest,
    family: *const u8,
    family_len: u32,
    features: *const GpuiGoTextFeature,
    feature_count: u32,
    fallbacks: *const u8,
    fallbacks_len: u32,
    out: *mut GpuiGoTextFontRecord,
) -> i32 {
    text_entry!(
        "text_font_resolve",
        text_font_resolve_body(
            request,
            family,
            family_len,
            features,
            feature_count,
            fallbacks,
            fallbacks_len,
            out
        )
    )
}

unsafe extern "system" fn text_font_add(
    data: *const u8,
    data_len: u32,
    out_generation: *mut u64,
) -> i32 {
    text_entry!("text_font_add", add_font_body(data, data_len, out_generation))
}

unsafe extern "system" fn text_font_metrics(
    font_id: u64,
    out: *mut GpuiGoTextFontMetricsRecord,
) -> i32 {
    text_entry!("text_font_metrics", text_font_metrics_body(font_id, out))
}

#[allow(clippy::too_many_arguments)]
unsafe extern "system" fn text_shape(
    text: *const u8,
    text_len: u32,
    run_records: *const GpuiGoTextRunRecord,
    run_count: u32,
    features: *const GpuiGoTextFeature,
    feature_count: u32,
    variations: *const GpuiGoTextVariation,
    variation_count: u32,
    strings: *const u8,
    strings_len: u32,
    font_size_bits: u32,
    wrap_width: u32,
    wrap_present: u32,
    line_clamp: u32,
    clamp_present: u32,
    out_handle: *mut u64,
) -> i32 {
    text_entry!(
        "text_shape",
        text_shape_body(
            text,
            text_len,
            run_records,
            run_count,
            features,
            feature_count,
            variations,
            variation_count,
            strings,
            strings_len,
            font_size_bits,
            wrap_width,
            wrap_present,
            line_clamp,
            clamp_present,
            out_handle
        )
    )
}

unsafe extern "system" fn text_layout(
    handle: u64,
    wrap_width: u32,
    wrap_present: u32,
    line_clamp: u32,
    clamp_present: u32,
) -> i32 {
    text_entry!(
        "text_layout",
        text_layout_body(handle, wrap_width, wrap_present, line_clamp, clamp_present)
    )
}

unsafe extern "system" fn text_run_info(
    handle: u64,
    run_index: u32,
    out: *mut GpuiGoTextRunRecord,
) -> i32 {
    text_entry!("text_run_info", text_run_info_body(handle, run_index, out))
}

unsafe extern "system" fn text_dispose(handle: u64) -> i32 {
    text_entry!("text_dispose", text_dispose_body(handle))
}

unsafe extern "system" fn text_layout_info(
    handle: u64,
    out: *mut GpuiGoTextLayoutRecord,
) -> i32 {
    text_entry!("text_layout_info", text_layout_info_body(handle, out))
}

unsafe extern "system" fn text_line_count(handle: u64, out: *mut u32) -> i32 {
    text_entry!("text_line_count", text_line_count_body(handle, out))
}

unsafe extern "system" fn text_line(
    handle: u64,
    index: u32,
    line_height_bits: u32,
    out: *mut GpuiGoTextLineRecord,
) -> i32 {
    text_entry!("text_line", text_line_body(handle, index, line_height_bits, out))
}

unsafe extern "system" fn text_caret(
    handle: u64,
    index: u32,
    affinity: u32,
    line_height_bits: u32,
    out: *mut GpuiGoTextCaretRecord,
) -> i32 {
    text_entry!(
        "text_caret",
        text_caret_body(handle, index, affinity, line_height_bits, out)
    )
}

unsafe extern "system" fn text_hit_test(
    handle: u64,
    x_bits: u32,
    y_bits: u32,
    line_height_bits: u32,
    out: *mut GpuiGoTextHitRecord,
) -> i32 {
    text_entry!(
        "text_hit_test",
        text_hit_test_body(handle, x_bits, y_bits, line_height_bits, out)
    )
}

#[allow(clippy::too_many_arguments)]
unsafe extern "system" fn text_selection_rects(
    handle: u64,
    start: u32,
    end: u32,
    line_height_bits: u32,
    rects: *mut GpuiGoTextSelectionRecord,
    capacity: u32,
    out_needed: *mut u32,
) -> i32 {
    text_entry!(
        "text_selection_rects",
        text_selection_rects_body(
            handle,
            start,
            end,
            line_height_bits,
            rects,
            capacity,
            out_needed
        )
    )
}

unsafe extern "system" fn text_cluster(
    handle: u64,
    index: u32,
    affinity: u32,
    side: u32,
    out: *mut GpuiGoTextClusterRecord,
) -> i32 {
    text_entry!("text_cluster", text_cluster_body(handle, index, affinity, side, out))
}

unsafe extern "system" fn text_fragments(
    handle: u64,
    records: *mut GpuiGoTextFragmentRecord,
    capacity: u32,
    out_needed: *mut u32,
) -> i32 {
    text_entry!(
        "text_fragments",
        text_fragments_body(handle, records, capacity, out_needed)
    )
}

unsafe extern "system" fn text_glyphs(
    handle: u64,
    records: *mut GpuiGoTextGlyphRecord,
    capacity: u32,
    out_needed: *mut u32,
) -> i32 {
    text_entry!("text_glyphs", text_glyphs_body(handle, records, capacity, out_needed))
}

// ---------------------------------------------------------------------------
// Tests (real system fonts: this machine has Segoe UI, Arial, Tahoma …)
// ---------------------------------------------------------------------------

#[cfg(test)]
pub(crate) mod tests {
    use super::*;

    /// Test-only stack access for the sibling glyph service's tests
    /// (they resolve and register fonts through the same global state,
    /// without leaking this module's private state types across the
    /// crate).
    pub(crate) fn test_resolve_and_register(family: &str) -> Option<FontId> {
        let mut state = lock_global();
        let stack = ensure_stack(&mut state);
        match stack.parley.font_id(&gpui::font(family)) {
            Ok(font_id) => {
                register_font(&mut state, &stack, font_id);
                Some(font_id)
            }
            Err(_) => None,
        }
    }

    /// Fontique/DirectWrite enumeration happens once per process on first
    /// use (the lazily created stack); these tests observe the real
    /// installed-font environment, as the ticket requires.
    const LINE_HEIGHT: f32 = 24.0;
    const FONT_SIZE: f32 = 16.0;

    /// The pinned construction appends service fallback families (Lilex,
    /// IBM Plex Sans, Arial) to every resolve chain, so a bogus family
    /// name RESOLVES through fallback on this machine (Arial is
    /// installed): that is the pinned fallback-selection behavior, and
    /// ERR_FONT_UNRESOLVED only fires when the whole chain fails.
    const BOGUS_FAMILY: &str = "Definitely Not A Real Family 4f81c0";

    /// Shapes with a retry: tests run in parallel threads and the capacity
    /// test briefly holds every slot, so a spurious ERR_SHAPING_LIMIT must
    /// be retried, not fatal.
    fn shape_with(text: &str, wrap: Option<f32>, clamp: Option<u32>) -> u64 {
        let run = GpuiGoTextRunRecord {
            len: text.len() as u32,
            family_offset: 0,
            family_len: b"Segoe UI".len() as u32,
            fallbacks_offset: 0,
            fallbacks_len: 0,
            weight_bits: 400.0f32.to_bits(),
            style: 0,
            feature_offset: 0,
            feature_count: 0,
            letter_spacing_present: 0,
            letter_spacing_bits: 0,
            word_break: 0,
            overflow_wrap: 0,
            text_wrap: 0,
            locale_offset: 0,
            locale_len: 0,
            font_width_present: 0,
            font_width_bits: 0,
            variation_offset: 0,
            variation_count: 0,
            opsz_present: 0,
            opsz_bits: 0,
            justify: 0,
        };
        let strings = b"Segoe UI";
        for _ in 0..200 {
            let mut handle = 0;
            let code = unsafe {
                text_shape(
                    text.as_ptr(),
                    text.len() as u32,
                    &run,
                    1,
                    std::ptr::null(),
                    0,
                    std::ptr::null(),
                    0,
                    strings.as_ptr(),
                    strings.len() as u32,
                    FONT_SIZE.to_bits(),
                    wrap.unwrap_or(0.0).to_bits(),
                    u32::from(wrap.is_some()),
                    clamp.unwrap_or(0),
                    u32::from(clamp.is_some()),
                    &mut handle,
                )
            };
            if code == text_status::OK {
                assert_ne!(handle, 0);
                return handle;
            }
            if code != text_status::ERR_SHAPING_LIMIT {
                panic!("text_shape failed for {text:?}: status {code}");
            }
            std::thread::sleep(std::time::Duration::from_millis(10));
        }
        panic!("text_shape never acquired a free slot for {text:?}");
    }

    fn shape_simple(text: &str) -> u64 {
        shape_with(text, None, None)
    }

    fn layout_info(handle: u64) -> GpuiGoTextLayoutRecord {
        let mut record = GpuiGoTextLayoutRecord {
            text_len: u32::MAX,
            font_size_bits: 0,
            wrap_present: 0,
            wrap_bits: 0,
            clamp_present: 0,
            line_clamp: 0,
            line_count: 0,
            fragment_count: 0,
            glyph_count: 0,
            width_bits: 0,
            ascent_bits: 0,
            descent_bits: 0,
            font_generation: 0,
            record_size: 0,
        };
        let code = unsafe { text_layout_info(handle, &mut record) };
        assert_eq!(code, text_status::OK);
        record
    }

    fn dump_fragments(handle: u64) -> Vec<GpuiGoTextFragmentRecord> {
        let mut needed = 0;
        let code = unsafe { text_fragments(handle, std::ptr::null_mut(), 0, &mut needed) };
        assert!(code == text_status::ERR_CAPACITY || code == text_status::OK);
        if needed == 0 {
            return Vec::new();
        }
        let mut records = vec![
            GpuiGoTextFragmentRecord {
                font_id: 0,
                font_size_bits: 0,
                x_start_bits: 0,
                x_end_bits: 0,
                glyph_start: 0,
                glyph_count: 0,
                record_size: 0
            };
            needed as usize
        ];
        let code = unsafe { text_fragments(handle, records.as_mut_ptr(), needed, &mut needed) };
        assert_eq!(code, text_status::OK);
        records
    }

    fn dump_glyphs(handle: u64) -> Vec<GpuiGoTextGlyphRecord> {
        let mut needed = 0;
        let code = unsafe { text_glyphs(handle, std::ptr::null_mut(), 0, &mut needed) };
        assert!(code == text_status::ERR_CAPACITY || code == text_status::OK);
        if needed == 0 {
            return Vec::new();
        }
        let mut records = vec![
            GpuiGoTextGlyphRecord {
                glyph_id: 0,
                x_bits: 0,
                y_bits: 0,
                is_emoji: 0,
                record_size: 0
            };
            needed as usize
        ];
        let code = unsafe { text_glyphs(handle, records.as_mut_ptr(), needed, &mut needed) };
        assert_eq!(code, text_status::OK);
        records
    }

    fn caret(handle: u64, index: u32) -> GpuiGoTextCaretRecord {
        let mut record = GpuiGoTextCaretRecord {
            index: 0,
            affinity: 0,
            present: 0,
            x_bits: 0,
            y_bits: 0,
            w_bits: 0,
            h_bits: 0,
            cluster_before_present: 0,
            cluster_before_start: 0,
            cluster_before_end: 0,
            cluster_after_present: 0,
            cluster_after_start: 0,
            cluster_after_end: 0,
            record_size: 0,
        };
        let code = unsafe { text_caret(handle, index, 0, LINE_HEIGHT.to_bits(), &mut record) };
        assert_eq!(code, text_status::OK);
        record
    }

    fn hit_test(handle: u64, x: f32, y: f32) -> GpuiGoTextHitRecord {
        let mut record =
            GpuiGoTextHitRecord { index: 0, affinity: 0, inside: 0, record_size: 0 };
        let code = unsafe {
            text_hit_test(handle, x.to_bits(), y.to_bits(), LINE_HEIGHT.to_bits(), &mut record)
        };
        assert_eq!(code, text_status::OK);
        record
    }

    fn bits(value: u32) -> f32 {
        f32::from_bits(value)
    }

    #[test]
    fn table_identity_and_self_checks() {
        assert_eq!(TEXT_TABLE.service_version, GPUI_GO_TEXT_SERVICE_VERSION);
        assert_eq!(
            TEXT_TABLE.size_of_table as usize,
            std::mem::size_of::<GpuiGoTextTable>()
        );
        assert_eq!(
            TEXT_TABLE.align_of_table as usize,
            std::mem::align_of::<GpuiGoTextTable>()
        );
        assert_eq!(TEXT_TABLE.max_shaping_handles, MAX_SHAPING_SLOTS);
        assert_eq!(TEXT_TABLE.max_text_bytes, MAX_TEXT_BYTES);
        for (name, slot) in [
            ("font_names", TEXT_TABLE.font_names.is_some()),
            ("font_resolve", TEXT_TABLE.font_resolve.is_some()),
            ("font_metrics", TEXT_TABLE.font_metrics.is_some()),
            ("shape", TEXT_TABLE.shape.is_some()),
            ("layout", TEXT_TABLE.layout.is_some()),
            ("dispose", TEXT_TABLE.dispose.is_some()),
            ("layout_info", TEXT_TABLE.layout_info.is_some()),
            ("line_count", TEXT_TABLE.line_count.is_some()),
            ("line", TEXT_TABLE.line.is_some()),
            ("caret", TEXT_TABLE.caret.is_some()),
            ("hit_test", TEXT_TABLE.hit_test.is_some()),
            ("selection_rects", TEXT_TABLE.selection_rects.is_some()),
            ("cluster", TEXT_TABLE.cluster.is_some()),
            ("fragments", TEXT_TABLE.fragments.is_some()),
            ("glyphs", TEXT_TABLE.glyphs.is_some()),
        ] {
            assert!(slot, "table slot {name} must be non-null");
        }
    }



    #[test]
    fn font_names_enumerate_system_families() {
        let mut count = 0;
        let mut needed = 0;
        // Capacity probe first (the Go call-twice protocol).
        let code = unsafe { text_font_names(std::ptr::null_mut(), 0, &mut count, &mut needed) };
        assert_eq!(code, text_status::ERR_CAPACITY);
        assert!(count > 10, "expected a real system font catalog, got {count}");
        let mut buf = vec![0u8; needed as usize];
        let code = unsafe { text_font_names(buf.as_mut_ptr(), needed, &mut count, &mut needed) };
        assert_eq!(code, text_status::OK);
        let packed = String::from_utf8_lossy(&buf);
        let names: Vec<&str> = packed.trim_end_matches('\0').split('\0').collect();
        assert_eq!(names.len(), count as usize);
        assert!(names.contains(&"Segoe UI"), "Segoe UI missing: {names:?}");
        // The pinned all_font_names appends the system-UI alias.
        assert!(names.contains(&".SystemUIFont"));
        // Too-small capacity copies nothing.
        let code = unsafe { text_font_names(std::ptr::null_mut(), 1, &mut count, &mut needed) };
        assert_eq!(code, text_status::ERR_CAPACITY);
    }

    #[test]
    fn font_resolve_identity_and_metrics() {
        let request = GpuiGoTextFontRequest {
            weight_bits: 400.0f32.to_bits(),
            style: 0,
            feature_count: 0,
            fallback_count: 0,
            reserved: [0; 3],
        };
        let mut out = GpuiGoTextFontRecord {
            font_id: 0,
            generation: 0,
            weight_bits: 0,
            style: 0,
            feature_count: 0,
            fallback_count: 0,
            family_found: 0,
            record_size: 0,
        };
        let code = unsafe {
            text_font_resolve(
                &request,
                b"Segoe UI".as_ptr(),
                b"Segoe UI".len() as u32,
                std::ptr::null(),
                0,
                std::ptr::null(),
                0,
                &mut out,
            )
        };
        assert_eq!(code, text_status::OK);
        assert_eq!(out.record_size as usize, std::mem::size_of::<GpuiGoTextFontRecord>());
        assert_ne!(out.font_id & FONT_ID_CANONICAL_BIT, 0, "canonical bit missing");
        assert_eq!(out.weight_bits, request.weight_bits);

        // Metrics for the resolved handle.
        let mut metrics = GpuiGoTextFontMetricsRecord {
            units_per_em: 0,
            ascent_bits: 0,
            descent_bits: 0,
            line_gap_bits: 0,
            underline_position_bits: 0,
            underline_thickness_bits: 0,
            cap_height_bits: 0,
            x_height_bits: 0,
            bbox_x_bits: 0,
            bbox_y_bits: 0,
            bbox_w_bits: 0,
            bbox_h_bits: 0,
            record_size: 0,
        };
        let code = unsafe { text_font_metrics(out.font_id, &mut metrics) };
        assert_eq!(code, text_status::OK);
        assert!(metrics.units_per_em > 0);
        assert!(bits(metrics.ascent_bits) > 0.0);
        assert!(bits(metrics.descent_bits) > 0.0);
        assert!(bits(metrics.x_height_bits) > 0.0);
        assert!(bits(metrics.x_height_bits) < bits(metrics.ascent_bits));

        // Determinism: resolving again yields the same canonical id.
        let mut again = out;
        let code = unsafe {
            text_font_resolve(
                &request,
                b"Segoe UI".as_ptr(),
                b"Segoe UI".len() as u32,
                std::ptr::null(),
                0,
                std::ptr::null(),
                0,
                &mut again,
            )
        };
        assert_eq!(code, text_status::OK);
        assert_eq!(again.font_id, out.font_id);

        // A family that cannot be matched directly resolves through the
        // pinned fallback chain (service fallbacks: Lilex, IBM Plex Sans,
        // Arial) — the pinned fallback-selection behavior. On a system
        // where the entire chain fails this is ERR_FONT_UNRESOLVED instead.
        let code = unsafe {
            text_font_resolve(
                &request,
                BOGUS_FAMILY.as_ptr(),
                BOGUS_FAMILY.len() as u32,
                std::ptr::null(),
                0,
                std::ptr::null(),
                0,
                &mut out,
            )
        };
        assert_eq!(code, text_status::OK, "fallback resolution expected on this machine");
        assert_ne!(out.font_id & FONT_ID_CANONICAL_BIT, 0);
        let mut metrics = GpuiGoTextFontMetricsRecord {
            units_per_em: 0,
            ascent_bits: 0,
            descent_bits: 0,
            line_gap_bits: 0,
            underline_position_bits: 0,
            underline_thickness_bits: 0,
            cap_height_bits: 0,
            x_height_bits: 0,
            bbox_x_bits: 0,
            bbox_y_bits: 0,
            bbox_w_bits: 0,
            bbox_h_bits: 0,
            record_size: 0,
        };
        let code = unsafe { text_font_metrics(out.font_id, &mut metrics) };
        assert_eq!(code, text_status::OK);

        // Unknown font ids are typed errors (the store's expect never
        // fires at the ABI).
        let mut metrics = GpuiGoTextFontMetricsRecord {
            units_per_em: 0,
            ascent_bits: 0,
            descent_bits: 0,
            line_gap_bits: 0,
            underline_position_bits: 0,
            underline_thickness_bits: 0,
            cap_height_bits: 0,
            x_height_bits: 0,
            bbox_x_bits: 0,
            bbox_y_bits: 0,
            bbox_w_bits: 0,
            bbox_h_bits: 0,
            record_size: 0,
        };
        let code = unsafe { text_font_metrics(1, &mut metrics) };
        assert_eq!(code, text_status::ERR_BAD_HANDLE);

        // The family-presence diagnostic: an installed family reports 1,
        // the bogus family reports 0 (the fallback answered — observable
        // now, still behaviorally the pinned silent fallback).
        assert_eq!(out.family_found, 0, "the bogus family must report family_found = 0");
        let mut installed = out;
        let code = unsafe {
            text_font_resolve(
                &request,
                b"Segoe UI".as_ptr(),
                b"Segoe UI".len() as u32,
                std::ptr::null(),
                0,
                std::ptr::null(),
                0,
                &mut installed,
            )
        };
        assert_eq!(code, text_status::OK);
        assert_eq!(installed.family_found, 1, "Segoe UI must report family_found = 1");
    }

    /// The pinned checkout's bundled Lilex face (SIL OFL; the same bytes
    /// the Go module commits under internal/native/testdata). Lilex is a
    /// service-fallback family NOT installed on this machine, so
    /// registering it is observable: the family appears in the catalog
    /// and resolves to Lilex itself instead of the Arial fallback.
    const LILEX_BYTES: &[u8] =
        include_bytes!("../../ce-source/assets/fonts/lilex/Lilex-Regular.ttf");

    /// Resolves a family through the ABI, returning (status, record).
    fn resolve_family(family: &str) -> (i32, GpuiGoTextFontRecord) {
        let request = GpuiGoTextFontRequest {
            weight_bits: 400.0f32.to_bits(),
            style: 0,
            feature_count: 0,
            fallback_count: 0,
            reserved: [0; 3],
        };
        let mut out = GpuiGoTextFontRecord {
            font_id: 0,
            generation: 0,
            weight_bits: 0,
            style: 0,
            feature_count: 0,
            fallback_count: 0,
            family_found: 0,
            record_size: 0,
        };
        let code = unsafe {
            text_font_resolve(
                &request,
                family.as_ptr(),
                family.len() as u32,
                std::ptr::null(),
                0,
                std::ptr::null(),
                0,
                &mut out,
            )
        };
        (code, out)
    }

    /// Adds font bytes through the ABI, returning (status, generation).
    fn add_font(data: &[u8]) -> (i32, u64) {
        let mut generation = 0u64;
        let code = unsafe {
            text_font_add(
                data.as_ptr(),
                data.len() as u32,
                &mut generation,
            )
        };
        (code, generation)
    }

    /// The font-names dump as a Vec<String> (the capacity protocol).
    fn font_names() -> Vec<String> {
        let mut count = 0u32;
        let mut needed = 0u32;
        let code =
            unsafe { text_font_names(std::ptr::null_mut(), 0, &mut count, &mut needed) };
        assert_eq!(code, text_status::ERR_CAPACITY);
        let mut buf = vec![0u8; needed as usize];
        let code = unsafe { text_font_names(buf.as_mut_ptr(), needed, &mut count, &mut needed) };
        assert_eq!(code, text_status::OK);
        String::from_utf8_lossy(&buf)
            .trim_end_matches('\0')
            .split('\0')
            .map(str::to_owned)
            .collect()
    }

    #[test]
    fn font_add_registers_family_and_makes_it_resolvable() {
        // Pre-registration state: Lilex is a service fallback, not an
        // installed family, so it resolves through the chain (to Arial on
        // this machine) with family_found = 0. A machine that HAS Lilex
        // installed reports 1 — both states are recorded honestly.
        let (code, before) = resolve_family("Lilex");
        assert_eq!(code, text_status::OK, "the fallback chain must resolve");
        assert_ne!(before.font_id & FONT_ID_CANONICAL_BIT, 0);
        assert!(
            !font_names().contains(&"Lilex".to_owned()) || before.family_found == 1,
            "Lilex listed in the catalog must report family_found = 1"
        );

        // Registration: the pinned add_fonts path, one blob per call.
        let (code, generation) = add_font(LILEX_BYTES);
        assert_eq!(code, text_status::OK, "registering the bundled Lilex face");
        assert!(generation > 0, "a successful registration bumps the generation");

        // The family is now in the catalog and resolves to Lilex itself.
        assert!(font_names().contains(&"Lilex".to_owned()));
        let (code, after) = resolve_family("Lilex");
        assert_eq!(code, text_status::OK);
        assert_eq!(after.family_found, 1, "the registered family must report family_found = 1");
        assert_ne!(after.font_id & FONT_ID_CANONICAL_BIT, 0);
        if before.family_found == 0 {
            assert_ne!(
                after.font_id, before.font_id,
                "registering Lilex must change the resolved id (was the Arial fallback)"
            );
        }

        // Re-registration of the same bytes succeeds (Fontique has no
        // duplicate-family rule on this path, unlike the SVG service's
        // path-keyed registry) and bumps the generation again.
        let (code, generation2) = add_font(LILEX_BYTES);
        assert_eq!(code, text_status::OK);
        assert!(generation2 > generation);

        // The re-resolve stays stable and the cache tracks the new
        // generation (no stale family-presence answers).
        let (code, again) = resolve_family("Lilex");
        assert_eq!(code, text_status::OK);
        assert_eq!(again.font_id, after.font_id);
        assert_eq!(again.family_found, 1);
    }

    #[test]
    fn font_add_rejects_bad_data_and_arguments() {
        let (_, generation) = add_font(LILEX_BYTES);

        // Bytes with no supported font face: the typed font-data error,
        // not a panic, and the catalog is untouched.
        let (code, after) = add_font(b"definitely not a font file");
        assert_eq!(code, text_status::ERR_FONT_DATA);
        assert_eq!(after, generation, "a rejected registration must not bump the generation");
        assert!(!font_names().contains(&"Definitely Not A Font 12345".to_owned()));

        // Empty and oversize data, null pointers.
        let (code, _) = add_font(&[]);
        assert_eq!(code, text_status::ERR_BAD_VALUE);
        let (code, _) = add_font(&[0u8; (MAX_FONT_BYTES + 1) as usize]);
        assert_eq!(code, text_status::ERR_BAD_VALUE);
        let code = unsafe { text_font_add(LILEX_BYTES.as_ptr(), LILEX_BYTES.len() as u32, std::ptr::null_mut()) };
        assert_eq!(code, text_status::ERR_NULL_ARG);
        let code = unsafe { text_font_add(std::ptr::null(), LILEX_BYTES.len() as u32, &mut 0u64) };
        assert_eq!(code, text_status::ERR_NULL_ARG);

        // A family that resolves nowhere reports the fallback through
        // family_found = 0 (the diagnosable silent fallback).
        let (code, bogus) = resolve_family("Definitely Not A Font 12345");
        assert_eq!(code, text_status::OK, "the pinned chain resolves through fallback");
        assert_eq!(bogus.family_found, 0);
    }

    #[test]
    fn registered_family_shapes_distinct_from_system_default() {
        let (code, _) = add_font(LILEX_BYTES);
        assert_eq!(code, text_status::OK);

        // Shape the same text with the registered family and with the
        // installed system family: different resolved fonts, different
        // glyph ids and different advances.
        const TEXT: &str = "The quick brown fox";
        let shape_with_family = |family: &str| -> (GpuiGoTextLayoutRecord, u64, Vec<GpuiGoTextGlyphRecord>) {
            let run = GpuiGoTextRunRecord {
                len: TEXT.len() as u32,
                family_offset: 0,
                family_len: family.len() as u32,
                fallbacks_offset: 0,
                fallbacks_len: 0,
                weight_bits: 400.0f32.to_bits(),
                style: 0,
                feature_offset: 0,
                feature_count: 0,
                letter_spacing_present: 0,
                letter_spacing_bits: 0,
                word_break: 0,
                overflow_wrap: 0,
                text_wrap: 0,
                locale_offset: 0,
                locale_len: 0,
                font_width_present: 0,
                font_width_bits: 0,
                variation_offset: 0,
                variation_count: 0,
                opsz_present: 0,
                opsz_bits: 0,
                justify: 0,
            };
            let strings = family.as_bytes().to_vec();
            let mut handle = 0u64;
            let code = unsafe {
                text_shape(
                    TEXT.as_ptr(),
                    TEXT.len() as u32,
                    &run,
                    1,
                    std::ptr::null(),
                    0,
                    std::ptr::null(),
                    0,
                    strings.as_ptr(),
                    strings.len() as u32,
                    FONT_SIZE.to_bits(),
                    0.0f32.to_bits(),
                    0,
                    0,
                    0,
                    &mut handle,
                )
            };
            assert_eq!(code, text_status::OK, "shaping with {family}");
            let info = layout_info(handle);
            let fragments = dump_fragments(handle);
            assert!(!fragments.is_empty(), "shaping with {family} produced no fragments");
            let font_id = fragments[0].font_id;
            let glyphs = dump_glyphs(handle);
            unsafe { text_dispose(handle) };
            (info, font_id, glyphs)
        };

        let (lilex_info, lilex_font, lilex_glyphs) = shape_with_family("Lilex");
        let (system_info, system_font, system_glyphs) = shape_with_family("Segoe UI");

        assert_ne!(lilex_font, system_font, "the registered family must resolve to its own face");
        assert_ne!(
            lilex_glyphs, system_glyphs,
            "the registered family must shape different glyphs than the system default"
        );
        assert!(bits(lilex_info.width_bits) > 0.0);
        assert!(bits(system_info.width_bits) > 0.0);
        // Deterministic: shaping again with the registered family is
        // bit-identical (the store interns, the layout is pure).
        let (repeat_info, repeat_font, repeat_glyphs) = shape_with_family("Lilex");
        assert_eq!(repeat_font, lilex_font);
        assert_eq!(repeat_glyphs, lilex_glyphs);
        assert_eq!(bits(repeat_info.width_bits), bits(lilex_info.width_bits));
    }

    #[test]
    fn shape_known_string_run_cluster_glyph_counts() {
        let text = "Hello, gpui-go world!";
        let handle = shape_simple(text);
        let info = layout_info(handle);
        assert_eq!(info.text_len, text.len() as u32);
        assert_eq!(bits(info.font_size_bits), FONT_SIZE);
        assert_eq!(info.wrap_present, 0);
        assert_eq!(info.line_count, 1);
        assert!(info.fragment_count >= 1);
        // No ligatures/combining in the fixture: one glyph per character
        // (the fixture is 21 characters).
        assert_eq!(info.glyph_count, text.chars().count() as u32);
        assert_eq!(info.glyph_count, 21);
        // Non-degenerate geometry.
        assert!(bits(info.width_bits) > 80.0, "width {}", bits(info.width_bits));
        assert!(bits(info.ascent_bits) > 8.0);
        assert!(bits(info.descent_bits) > 2.0);

        let fragments = dump_fragments(handle);
        assert_eq!(fragments.len() as u32, info.fragment_count);
        let glyphs = dump_glyphs(handle);
        assert_eq!(glyphs.len() as u32, info.glyph_count);
        // Fragment bookkeeping partitions the glyph dump.
        let covered: u32 = fragments.iter().map(|f| f.glyph_count).sum();
        assert_eq!(covered, info.glyph_count);
        // Glyph ids are real ids (non-zero for visible text) and positions
        // advance (line-local x, baseline-relative y).
        let mut positions: Vec<f32> = Vec::with_capacity(glyphs.len());
        for glyph in &glyphs {
            assert_ne!(glyph.glyph_id, 0);
            let (x, y) = (bits(glyph.x_bits), bits(glyph.y_bits));
            assert!(x.is_finite() && y.is_finite());
            positions.push(x);
        }
        for pair in positions.windows(2) {
            assert!(
                pair[1] >= pair[0] - 1e-4,
                "LTR glyph x positions must not regress: {positions:?}"
            );
        }
        assert!(bits(fragments[0].x_start_bits) <= 0.5);
        assert!(bits(fragments[0].x_end_bits) > 50.0);
        // The fragment font is resolvable through the font registry.
        let mut metrics = GpuiGoTextFontMetricsRecord {
            units_per_em: 0,
            ascent_bits: 0,
            descent_bits: 0,
            line_gap_bits: 0,
            underline_position_bits: 0,
            underline_thickness_bits: 0,
            cap_height_bits: 0,
            x_height_bits: 0,
            bbox_x_bits: 0,
            bbox_y_bits: 0,
            bbox_w_bits: 0,
            bbox_h_bits: 0,
            record_size: 0,
        };
        let code = unsafe { text_font_metrics(fragments[0].font_id, &mut metrics) };
        assert_eq!(code, text_status::OK);
        assert!(metrics.units_per_em > 0);

        unsafe { text_dispose(handle) };
    }

    #[test]
    fn wrap_and_clamp_behavior() {
        let text = "The quick brown fox jumps over the lazy dog";
        let handle = shape_with(text, Some(100.0), None);
        let info = layout_info(handle);
        assert_eq!(info.wrap_present, 1);
        assert_eq!(bits(info.wrap_bits), 100.0);
        assert!(info.line_count > 1, "expected wrapping, got {}", info.line_count);

        // Line ranges partition the text.
        let mut covered = 0usize;
        let mut last_end = 0usize;
        for index in 0..info.line_count {
            let mut record = GpuiGoTextLineRecord {
                line_index: 0,
                text_start: 0,
                text_end: 0,
                fragment_start: 0,
                fragment_end: 0,
                advance_width_bits: 0,
                line_height_bits: 0,
                baseline_bits: 0,
                record_size: 0,
            };
            let code = unsafe { text_line(handle, index, LINE_HEIGHT.to_bits(), &mut record) };
            assert_eq!(code, text_status::OK);
            assert_eq!(record.line_index, index);
            assert_eq!(record.text_start, last_end as u32);
            last_end = record.text_end as usize;
            covered += (record.text_end - record.text_start) as usize;
            // The pinned baseline derivation: (lh - ascent - descent)/2 + ascent.
            let ascent = bits(info.ascent_bits);
            let descent = bits(info.descent_bits);
            let expected = (LINE_HEIGHT - ascent - descent) / 2.0 + ascent;
            assert!((bits(record.baseline_bits) - expected).abs() < 1e-3);
            // Every wrapped row consumes width within the constraint (the
            // last row may be short).
            if index + 1 < info.line_count {
                assert!(bits(record.advance_width_bits) <= 100.0 + 1.0);
            }
        }
        assert_eq!(covered, text.len());

        // Re-layout without wrapping collapses to one row.
        let code = unsafe { text_layout(handle, 0, 0, 0, 0) };
        assert_eq!(code, text_status::OK);
        let info = layout_info(handle);
        assert_eq!(info.wrap_present, 0);
        assert_eq!(info.line_count, 1);

        // Line clamp bounds the row count.
        let clamped = shape_with(text, Some(100.0), Some(2));
        let info = layout_info(clamped);
        assert_eq!(info.clamp_present, 1);
        assert_eq!(info.line_clamp, 2);
        assert_eq!(info.line_count, 2);
        unsafe { text_dispose(clamped) };
        unsafe { text_dispose(handle) };
    }

    #[test]
    fn caret_monotonicity_single_line() {
        let text = "abcdefghij";
        let handle = shape_simple(text);
        let mut previous_x = f32::NEG_INFINITY;
        for (offset, _) in text.char_indices() {
            let record = caret(handle, offset as u32);
            assert_eq!(record.present, 1, "caret at {offset} must have geometry");
            assert_eq!(record.y_bits, 0.0f32.to_bits(), "single line: y must be 0");
            let x = bits(record.x_bits);
            assert!(x >= previous_x, "caret x regressed at {offset}");
            assert!(x > -1e-3);
            previous_x = x;
            // Cluster granularity: the grapheme after a boundary starts
            // exactly at the boundary for simple ASCII.
            assert_eq!(record.cluster_after_present, 1);
            assert_eq!(record.cluster_after_start, offset as u32);
            assert_eq!(record.cluster_after_end, offset as u32 + 1);
            assert_eq!(record.cluster_before_present, u32::from(offset > 0));
        }
        // End caret.
        let record = caret(handle, text.len() as u32);
        assert_eq!(record.present, 1);
        assert!(bits(record.x_bits) > 50.0);
        assert_eq!(record.cluster_before_end, text.len() as u32);
        unsafe { text_dispose(handle) };
    }

    #[test]
    fn hit_test_round_trip() {
        let text = "round trip geometry";
        let handle = shape_simple(text);
        let info = layout_info(handle);
        for (offset, _) in text.char_indices().skip(2).take(12) {
            let record = caret(handle, offset as u32);
            assert_eq!(record.present, 1);
            let (x, y) = (bits(record.x_bits), bits(record.y_bits));
            // Hit the vertical middle of the caret's row at the caret's x.
            let hit = hit_test(handle, x, y + LINE_HEIGHT / 2.0);
            assert_eq!(hit.inside, 1, "hit at caret position must be inside");
            // Round trip: offset -> position -> offset -> position.
            let round = caret(handle, hit.index);
            assert_eq!(round.present, 1);
            assert_eq!(bits(round.x_bits), x, "position round trip failed at {offset}");
            assert_eq!(bits(round.y_bits), y);
        }
        // A point far outside maps to the edge caret (Err path).
        let hit = hit_test(handle, 1e6, 1e6);
        assert_eq!(hit.inside, 0);
        let hit = hit_test(handle, -1e6, 5.0);
        assert_eq!(hit.inside, 0);
        assert!(bits(info.width_bits) > 0.0);
        unsafe { text_dispose(handle) };
    }

    #[test]
    fn selection_rects_nonempty_for_mid_line_range() {
        let text = "select some geometry here";
        let handle = shape_simple(text);
        let mut needed = 0;
        let code = unsafe {
            text_selection_rects(
                handle,
                7,
                14,
                LINE_HEIGHT.to_bits(),
                std::ptr::null_mut(),
                0,
                &mut needed,
            )
        };
        assert_eq!(code, text_status::ERR_CAPACITY);
        assert!(needed >= 1, "mid-line selection must have geometry");
        let mut records = vec![
            GpuiGoTextSelectionRecord {
                x_bits: 0,
                y_bits: 0,
                w_bits: 0,
                h_bits: 0,
                record_size: 0
            };
            needed as usize
        ];
        let code = unsafe {
            text_selection_rects(
                handle,
                7,
                14,
                LINE_HEIGHT.to_bits(),
                records.as_mut_ptr(),
                needed,
                &mut needed,
            )
        };
        assert_eq!(code, text_status::OK);
        assert_eq!(records.len(), 1);
        let rect = records[0];
        assert!(bits(rect.w_bits) > 0.0);
        assert!((bits(rect.h_bits) - LINE_HEIGHT).abs() < 1e-3);
        assert!(bits(rect.y_bits) >= 0.0 && bits(rect.y_bits) < LINE_HEIGHT);
        assert!(bits(rect.x_bits) >= 0.0);

        // An empty range produces no rectangles (the pinned rule).
        let mut needed = 0;
        let code = unsafe {
            text_selection_rects(
                handle,
                7,
                7,
                LINE_HEIGHT.to_bits(),
                std::ptr::null_mut(),
                0,
                &mut needed,
            )
        };
        assert_eq!(code, text_status::OK);
        assert_eq!(needed, 0);
        unsafe { text_dispose(handle) };
    }



    #[test]
    fn cluster_boundaries_multibyte_graphemes() {
        // 'a' + combining acute (U+0301, 2 UTF-8 bytes: one grapheme
        // 0..3), 'z' (3..4), a flag pair (4..12, one grapheme), 'w'
        // (12..13).
        let text = "a\u{0301}z\u{1F1FA}\u{1F1F8}w";
        assert_eq!(text.len(), 13);
        let handle = shape_simple(text);
        // Byte 0: the grapheme after is 0..3 ("a" + combining acute).
        let record = caret(handle, 0);
        assert_eq!(record.cluster_after_start, 0);
        assert_eq!(record.cluster_after_end, 3);
        // Byte 2 is inside the combining mark: not a boundary.
        let mut cluster = GpuiGoTextClusterRecord {
            present: 0,
            start: 0,
            end: 0,
            record_size: 0,
        };
        let code = unsafe { text_cluster(handle, 2, 0, 1, &mut cluster) };
        assert_eq!(code, text_status::ERR_BAD_VALUE);
        // Byte 3 is 'z' (1 byte).
        let record = caret(handle, 3);
        assert_eq!(record.cluster_after_start, 3);
        assert_eq!(record.cluster_after_end, 4);
        // Byte 4 starts the flag pair: 8 bytes.
        let record = caret(handle, 4);
        assert_eq!(record.cluster_after_start, 4);
        assert_eq!(record.cluster_after_end, 12);
        // The middle of the flag pair is not a boundary: typed error.
        let code = unsafe { text_cluster(handle, 6, 0, 1, &mut cluster) };
        assert_eq!(code, text_status::ERR_BAD_VALUE);
        // Byte 0 looking before: nothing.
        let code = unsafe { text_cluster(handle, 0, 0, 0, &mut cluster) };
        assert_eq!(code, text_status::OK);
        assert_eq!(cluster.present, 0);
        // Byte 12 looking before: the flag grapheme 4..12.
        let code = unsafe { text_cluster(handle, 12, 0, 0, &mut cluster) };
        assert_eq!(code, text_status::OK);
        assert_eq!(cluster.present, 1);
        assert_eq!(cluster.start, 4);
        assert_eq!(cluster.end, 12);
        unsafe { text_dispose(handle) };
    }

    #[test]
    fn multi_run_fragments_carry_resolved_fonts() {
        let text = "plainboldplain";
        // Two runs: "plain" (bytes 0..5) Segoe UI, "boldplain" (5..14)
        // Arial bold.
        let strings = b"Segoe UI\0Arial";
        let runs = [
            GpuiGoTextRunRecord {
                len: 5,
                family_offset: 0,
                family_len: 8,
                fallbacks_offset: 0,
                fallbacks_len: 0,
                weight_bits: 400.0f32.to_bits(),
                style: 0,
                feature_offset: 0,
                feature_count: 0,
                letter_spacing_present: 0,
                letter_spacing_bits: 0,
                word_break: 0,
                overflow_wrap: 0,
                text_wrap: 0,
                locale_offset: 0,
                locale_len: 0,
                font_width_present: 0,
                font_width_bits: 0,
                variation_offset: 0,
                variation_count: 0,
                opsz_present: 0,
                opsz_bits: 0,
                justify: 0,
            },
            GpuiGoTextRunRecord {
                len: 9,
                family_offset: 9,
                family_len: 5,
                fallbacks_offset: 0,
                fallbacks_len: 0,
                weight_bits: 700.0f32.to_bits(),
                style: 0,
                feature_offset: 0,
                feature_count: 0,
                letter_spacing_present: 0,
                letter_spacing_bits: 0,
                word_break: 0,
                overflow_wrap: 0,
                text_wrap: 0,
                locale_offset: 0,
                locale_len: 0,
                font_width_present: 0,
                font_width_bits: 0,
                variation_offset: 0,
                variation_count: 0,
                opsz_present: 0,
                opsz_bits: 0,
                justify: 0,
            },
        ];
        let mut handle = 0;
        let code = unsafe {
            text_shape(
                text.as_ptr(),
                text.len() as u32,
                runs.as_ptr(),
                2,
                std::ptr::null(),
                0,
                std::ptr::null(),
                0,
                strings.as_ptr(),
                strings.len() as u32,
                FONT_SIZE.to_bits(),
                0,
                0,
                0,
                0,
                &mut handle,
            )
        };
        assert_eq!(code, text_status::OK);
        let fragments = dump_fragments(handle);
        assert!(fragments.len() >= 2, "two styled runs must shape apart");
        // The two runs resolve to distinct canonical font ids (different
        // families/weights).
        let first = fragments[0].font_id;
        assert!(
            fragments.iter().any(|fragment| fragment.font_id != first),
            "expected at least two distinct resolved fonts"
        );
        // Fragment x ranges tile the line.
        let mut x = bits(fragments[0].x_start_bits);
        for fragment in &fragments {
            assert!((bits(fragment.x_start_bits) - x).abs() < 1e-2);
            x = bits(fragment.x_end_bits);
        }
        unsafe { text_dispose(handle) };
    }

    #[test]
    fn shaping_validation_rejects_bad_requests() {
        // Run coverage mismatch.
        let run = GpuiGoTextRunRecord {
            len: 3,
            family_offset: 0,
            family_len: 8,
            fallbacks_offset: 0,
            fallbacks_len: 0,
            weight_bits: 400.0f32.to_bits(),
            style: 0,
            feature_offset: 0,
            feature_count: 0,
            letter_spacing_present: 0,
            letter_spacing_bits: 0,
            word_break: 0,
            overflow_wrap: 0,
            text_wrap: 0,
            locale_offset: 0,
            locale_len: 0,
            font_width_present: 0,
            font_width_bits: 0,
            variation_offset: 0,
            variation_count: 0,
            opsz_present: 0,
            opsz_bits: 0,
            justify: 0,
        };
        let strings = b"Segoe UI";
        let mut handle = 0;
        let code = unsafe {
            text_shape(
                b"hello".as_ptr(),
                5,
                &run,
                1,
                std::ptr::null(),
                0,
                std::ptr::null(),
                0,
                strings.as_ptr(),
                strings.len() as u32,
                FONT_SIZE.to_bits(),
                0,
                0,
                0,
                0,
                &mut handle,
            )
        };
        assert_eq!(code, text_status::ERR_BAD_VALUE);

        // Non-UTF-8 text.
        let code = unsafe {
            text_shape(
                [0xFFu8, 0xFE].as_ptr(),
                2,
                &GpuiGoTextRunRecord { len: 2, ..run },
                1,
                std::ptr::null(),
                0,
                std::ptr::null(),
                0,
                strings.as_ptr(),
                strings.len() as u32,
                FONT_SIZE.to_bits(),
                0,
                0,
                0,
                0,
                &mut handle,
            )
        };
        assert_eq!(code, text_status::ERR_BAD_VALUE);

        // A family nothing in the chain can match... resolves through the
        // pinned fallback chain on this machine (Arial installed): the
        // shape succeeds with the fallback font. ERR_FONT_UNRESOLVED fires
        // only when the whole chain fails (the typed guard for that case).
        let bad_strings = b"No Such Family Zz9x";
        let code = unsafe {
            text_shape(
                b"hi".as_ptr(),
                2,
                &GpuiGoTextRunRecord { len: 2, family_len: 19, ..run },
                1,
                std::ptr::null(),
                0,
                std::ptr::null(),
                0,
                bad_strings.as_ptr(),
                bad_strings.len() as u32,
                FONT_SIZE.to_bits(),
                0,
                0,
                0,
                0,
                &mut handle,
            )
        };
        assert_eq!(code, text_status::OK, "fallback resolution expected on this machine");
        unsafe { text_dispose(handle) };

        // Invalid feature tag bytes.
        let feature = GpuiGoTextFeature { tag: u32::from_be_bytes([0x01, b'l', b'i', b'g']), value: 1 };
        let code = unsafe {
            text_shape(
                b"hi".as_ptr(),
                2,
                &GpuiGoTextRunRecord { len: 2, feature_count: 1, ..run },
                1,
                &feature,
                1,
                std::ptr::null(),
                0,
                strings.as_ptr(),
                strings.len() as u32,
                FONT_SIZE.to_bits(),
                0,
                0,
                0,
                0,
                &mut handle,
            )
        };
        assert_eq!(code, text_status::ERR_BAD_VALUE);

        // Feature value over u16 (the adapter's narrowing).
        let feature = GpuiGoTextFeature { tag: u32::from_be_bytes(*b"liga"), value: 70_000 };
        let code = unsafe {
            text_shape(
                b"hi".as_ptr(),
                2,
                &GpuiGoTextRunRecord { len: 2, feature_count: 1, ..run },
                1,
                &feature,
                1,
                std::ptr::null(),
                0,
                strings.as_ptr(),
                strings.len() as u32,
                FONT_SIZE.to_bits(),
                0,
                0,
                0,
                0,
                &mut handle,
            )
        };
        assert_eq!(code, text_status::ERR_BAD_VALUE);

        // Non-finite font size.
        let code = unsafe {
            text_shape(
                b"hi".as_ptr(),
                2,
                &run,
                1,
                std::ptr::null(),
                0,
                std::ptr::null(),
                0,
                strings.as_ptr(),
                strings.len() as u32,
                f32::NAN.to_bits(),
                0,
                0,
                0,
                0,
                &mut handle,
            )
        };
        assert_eq!(code, text_status::ERR_BAD_VALUE);
    }

    #[test]
    fn empty_text_shapes_one_empty_line() {
        // The pinned behavior: layout_line("") produces one visual line
        // with range 0..0, width 0 and no fragments.
        let handle = shape_simple("");
        let info = layout_info(handle);
        assert_eq!(info.text_len, 0);
        assert_eq!(info.line_count, 1);
        assert_eq!(info.fragment_count, 0);
        assert_eq!(info.glyph_count, 0);
        assert_eq!(bits(info.width_bits), 0.0);
        let fragments = dump_fragments(handle);
        assert!(fragments.is_empty());
        // The pinned ParleyLayout::caret_bounds returns a zero-width
        // bounds for the empty document's caret.
        let record = caret(handle, 0);
        assert_eq!(record.present, 1, "empty text caret still has geometry in this pin");
        assert_eq!(record.w_bits, 0.0f32.to_bits());
        unsafe { text_dispose(handle) };
    }

    #[test]
    fn newline_text_shapes_multiple_lines() {
        let text = "one\ntwo";
        let handle = shape_simple(text);
        let info = layout_info(handle);
        assert_eq!(info.line_count, 2, "hard newline must produce two rows");
        let mut first = GpuiGoTextLineRecord {
            line_index: 0,
            text_start: 0,
            text_end: 0,
            fragment_start: 0,
            fragment_end: 0,
            advance_width_bits: 0,
            line_height_bits: 0,
            baseline_bits: 0,
            record_size: 0,
        };
        let code = unsafe { text_line(handle, 0, LINE_HEIGHT.to_bits(), &mut first) };
        assert_eq!(code, text_status::OK);
        assert_eq!(first.text_start, 0);
        // The pinned multi-paragraph merge keeps the separator in the
        // paragraph's last visual row (text_system.rs: "Keep separators in
        // document indices without adding native trailing rows"), so row
        // 1 covers "one" + "\n" (0..4).
        assert_eq!(first.text_end, 4);
        // The caret after the newline sits on row 2.
        let record = caret(handle, 4);
        assert_eq!(record.present, 1);
        assert!((bits(record.y_bits) - LINE_HEIGHT).abs() < 1e-3);
        unsafe { text_dispose(handle) };
    }

    #[test]
    fn bidi_paragraph_shapes_and_hit_tests() {
        let text = "abc \u{5D9}\u{5D8} \u{5E2}\u{5D1}\u{5E8}\u{5D9}\u{5EA} def";
        let handle = shape_simple(text);
        let info = layout_info(handle);
        assert_eq!(info.line_count, 1);
        assert!(info.glyph_count > 5);
        // Carets exist at the logical boundaries, including inside the RTL
        // run (bidi resolves per cluster; the geometry is observable even
        // though direction is not part of the pinned output records).
        let hebrew_start = text.find('\u{5D9}').expect("hebrew offset");
        let record = caret(handle, hebrew_start as u32);
        assert_eq!(record.present, 1);
        let record = caret(handle, text.len() as u32);
        assert_eq!(record.present, 1);
        // Hit testing inside the line returns a boundary on a cluster.
        let hit = hit_test(handle, bits(record.x_bits) / 2.0, LINE_HEIGHT / 2.0);
        // (inside may be 0 or 1 depending on x; must not error either way)
        let _ = hit.inside;
        unsafe { text_dispose(handle) };
    }

    #[test]
    fn stale_bad_handles_and_capacity_bound() {
        let handle = shape_simple("capacity probe");
        let code = unsafe { text_dispose(handle) };
        assert_eq!(code, text_status::OK);
        // Disposed handle is stale for every entry.
        let mut count = 0;
        assert_eq!(
            unsafe { text_line_count(handle, &mut count) },
            text_status::ERR_STALE_HANDLE
        );
        let mut info = GpuiGoTextLayoutRecord {
            text_len: 0,
            font_size_bits: 0,
            wrap_present: 0,
            wrap_bits: 0,
            clamp_present: 0,
            line_clamp: 0,
            line_count: 0,
            fragment_count: 0,
            glyph_count: 0,
            width_bits: 0,
            ascent_bits: 0,
            descent_bits: 0,
            font_generation: 0,
            record_size: 0,
        };
        assert_eq!(
            unsafe { text_layout_info(handle, &mut info) },
            text_status::ERR_STALE_HANDLE
        );
        // Malformed handles: a never-issued generation is stale (or bad
        // while the slot does not exist yet), and a slot index beyond the
        // MAX_SHAPING_SLOTS bound is always bad.
        let (slot, generation) = shaping_handle_parts(handle);
        let bogus = make_shaping_handle(slot, generation.wrapping_add(5));
        let observed = unsafe { text_line_count(bogus, &mut count) };
        assert!(
            observed == text_status::ERR_STALE_HANDLE
                || observed == text_status::ERR_BAD_HANDLE,
            "never-issued generation must be rejected: {observed}"
        );
        let bogus = make_shaping_handle(0x00FF_FFFF, 1);
        assert_eq!(
            unsafe { text_line_count(bogus, &mut count) },
            text_status::ERR_BAD_HANDLE
        );

        // Capacity: fill the slots up to MAX_SHAPING_SLOTS. A sibling test
        // that panicked without disposing leaks occupied slots toward the
        // same bound (the bound is on TOTAL live handles), so the fill
        // stops at the first ERR_SHAPING_LIMIT.
        let mut live: Vec<u64> = Vec::new();
        let fill_run = GpuiGoTextRunRecord {
            len: 1,
            family_offset: 0,
            family_len: 8,
            fallbacks_offset: 0,
            fallbacks_len: 0,
            weight_bits: 400.0f32.to_bits(),
            style: 0,
            feature_offset: 0,
            feature_count: 0,
            letter_spacing_present: 0,
            letter_spacing_bits: 0,
            word_break: 0,
            overflow_wrap: 0,
            text_wrap: 0,
            locale_offset: 0,
            locale_len: 0,
            font_width_present: 0,
            font_width_bits: 0,
            variation_offset: 0,
            variation_count: 0,
            opsz_present: 0,
            opsz_bits: 0,
            justify: 0,
        };
        let strings = b"Segoe UI";
        loop {
            let mut probe = 0;
            let code = unsafe {
                text_shape(
                    b"x".as_ptr(),
                    1,
                    &fill_run,
                    1,
                    std::ptr::null(),
                    0,
                    std::ptr::null(),
                    0,
                    strings.as_ptr(),
                    strings.len() as u32,
                    FONT_SIZE.to_bits(),
                    0,
                    0,
                    0,
                    0,
                    &mut probe,
                )
            };
            if code == text_status::ERR_SHAPING_LIMIT {
                break;
            }
            assert_eq!(code, text_status::OK, "fill shape failed: {code}");
            live.push(probe);
            if live.len() >= MAX_SHAPING_SLOTS as usize {
                break;
            }
        }
        assert!(live.len() <= MAX_SHAPING_SLOTS as usize);
        let mut overflow = 0;
        let run = GpuiGoTextRunRecord {
            len: 1,
            family_offset: 0,
            family_len: 8,
            fallbacks_offset: 0,
            fallbacks_len: 0,
            weight_bits: 400.0f32.to_bits(),
            style: 0,
            feature_offset: 0,
            feature_count: 0,
            letter_spacing_present: 0,
            letter_spacing_bits: 0,
            word_break: 0,
            overflow_wrap: 0,
            text_wrap: 0,
            locale_offset: 0,
            locale_len: 0,
            font_width_present: 0,
            font_width_bits: 0,
            variation_offset: 0,
            variation_count: 0,
            opsz_present: 0,
            opsz_bits: 0,
            justify: 0,
        };
        let strings = b"Segoe UI";
        let code = unsafe {
            text_shape(
                b"x".as_ptr(),
                1,
                &run,
                1,
                std::ptr::null(),
                0,
                std::ptr::null(),
                0,
                strings.as_ptr(),
                strings.len() as u32,
                FONT_SIZE.to_bits(),
                0,
                0,
                0,
                0,
                &mut overflow,
            )
        };
        assert_eq!(code, text_status::ERR_SHAPING_LIMIT);
        // Disposing one frees a slot.
        let freed = live.pop().expect("live handle");
        let code = unsafe { text_dispose(freed) };
        assert_eq!(code, text_status::OK);
        let code = unsafe {
            text_shape(
                b"y".as_ptr(),
                1,
                &GpuiGoTextRunRecord { len: 1, ..run },
                1,
                std::ptr::null(),
                0,
                std::ptr::null(),
                0,
                strings.as_ptr(),
                strings.len() as u32,
                FONT_SIZE.to_bits(),
                0,
                0,
                0,
                0,
                &mut overflow,
            )
        };
        assert_eq!(code, text_status::OK);
        for handle in live {
            unsafe { text_dispose(handle) };
        }
        unsafe { text_dispose(overflow) };
    }





    #[test]
    fn features_and_letter_spacing_change_geometry() {
        // Segoe UI, Times New Roman and Georgia shape "fi" without a
        // ligature through this stack; Cambria carries one which liga=0
        // disables — the feature cases use that face.
        let text = "fi fi";
        let times = b"Cambria";
        let times_run = GpuiGoTextRunRecord {
            len: text.len() as u32,
            family_offset: 0,
            family_len: times.len() as u32,
            fallbacks_offset: 0,
            fallbacks_len: 0,
            weight_bits: 400.0f32.to_bits(),
            style: 0,
            feature_offset: 0,
            feature_count: 0,
            letter_spacing_present: 0,
            letter_spacing_bits: 0,
            word_break: 0,
            overflow_wrap: 0,
            text_wrap: 0,
            locale_offset: 0,
            locale_len: 0,
            font_width_present: 0,
            font_width_bits: 0,
            variation_offset: 0,
            variation_count: 0,
            opsz_present: 0,
            opsz_bits: 0,
            justify: 0,
        };
        let mut times_handle = 0;
        let code = unsafe {
            text_shape(
                text.as_ptr(),
                text.len() as u32,
                &times_run,
                1,
                std::ptr::null(),
                0,
                std::ptr::null(),
                0,
                times.as_ptr(),
                times.len() as u32,
                FONT_SIZE.to_bits(),
                0,
                0,
                0,
                0,
                &mut times_handle,
            )
        };
        assert_eq!(code, text_status::OK);
        let plain = layout_info(times_handle);
        unsafe { text_dispose(times_handle) };

        // 'liga' explicitly on vs explicitly off: Times New Roman's fi
        // ligature merges glyphs when on, and shapes them separately when
        // off (the default shaping already applies liga).
        let run = GpuiGoTextRunRecord {
            len: text.len() as u32,
            family_offset: 0,
            family_len: times.len() as u32,
            fallbacks_offset: 0,
            fallbacks_len: 0,
            weight_bits: 400.0f32.to_bits(),
            style: 0,
            feature_offset: 0,
            feature_count: 1,
            letter_spacing_present: 0,
            letter_spacing_bits: 0,
            word_break: 0,
            overflow_wrap: 0,
            text_wrap: 0,
            locale_offset: 0,
            locale_len: 0,
            font_width_present: 0,
            font_width_bits: 0,
            variation_offset: 0,
            variation_count: 0,
            opsz_present: 0,
            opsz_bits: 0,
            justify: 0,
        };
        let strings = times;
        // 'liga' on vs off: Cambria's liga table substitutes a different
        // 'f' glyph (observed: 976 with, 136 without, at equal advance) —
        // feature settings demonstrably reach the shaper through the
        // pinned FontFeatures path.
        let mut first_ids: Vec<Vec<u32>> = Vec::new();
        for value in [1u32, 0u32] {
            let feature =
                GpuiGoTextFeature { tag: u32::from_be_bytes(*b"liga"), value };
            let mut handle = 0;
            let code = unsafe {
                text_shape(
                    text.as_ptr(),
                    text.len() as u32,
                    &run,
                    1,
                    &feature,
                    1,
                    std::ptr::null(),
                    0,
                    strings.as_ptr(),
                    strings.len() as u32,
                    FONT_SIZE.to_bits(),
                    0,
                    0,
                    0,
                    0,
                    &mut handle,
                )
            };
            assert_eq!(code, text_status::OK);
            let shaped = layout_info(handle);
            assert_eq!(shaped.glyph_count, plain.glyph_count);
            let glyphs = dump_glyphs(handle);
            first_ids.push(glyphs.iter().map(|glyph| glyph.glyph_id).collect());
            unsafe { text_dispose(handle) };
        }
        assert_ne!(
            first_ids[0], first_ids[1],
            "liga on/off must shape 'fi' differently"
        );

        // Letter spacing widens the line.
        let run = GpuiGoTextRunRecord {
            len: text.len() as u32,
            family_offset: 0,
            family_len: times.len() as u32,
            fallbacks_offset: 0,
            fallbacks_len: 0,
            weight_bits: 400.0f32.to_bits(),
            style: 0,
            feature_offset: 0,
            feature_count: 0,
            letter_spacing_present: 1,
            letter_spacing_bits: 2.0f32.to_bits(),
            word_break: 0,
            overflow_wrap: 0,
            text_wrap: 0,
            locale_offset: 0,
            locale_len: 0,
            font_width_present: 0,
            font_width_bits: 0,
            variation_offset: 0,
            variation_count: 0,
            opsz_present: 0,
            opsz_bits: 0,
            justify: 0,
        };
        let mut handle = 0;
        let code = unsafe {
            text_shape(
                text.as_ptr(),
                text.len() as u32,
                &run,
                1,
                std::ptr::null(),
                0,
                std::ptr::null(),
                0,
                strings.as_ptr(),
                strings.len() as u32,
                FONT_SIZE.to_bits(),
                0,
                0,
                0,
                0,
                &mut handle,
            )
        };
        assert_eq!(code, text_status::OK);
        let spaced = layout_info(handle);
        assert!(
            bits(spaced.width_bits) > bits(plain.width_bits),
            "letter spacing must widen the line"
        );
        unsafe { text_dispose(handle) };
    }
}
