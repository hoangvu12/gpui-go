//! gpui-go native layout service (ticket06).
//!
//! Implements the layout ABI documented in [`LAYOUT_ABI.md`]: a registry of
//! Taffy 0.13.0 layout engines behind `#[repr(C)]` records and
//! `extern "system"` function tables. The service table is installed in
//! **reserved slot 1** of the bootstrap `GpuiGoAbiTable` and advertised with
//! capability bit 1 (`layout-taffy-0-13-0`).
//!
//! Semantics follow the pinned gpui-CE adapter exactly
//! (`crates/gpui/src/taffy.rs`, `crates/gpui/src/style.rs` at commit
//! `254b5dbd…`): the style translation is `into_taffy_style` restricted to
//! the GPUI-forwarded field set, Taffy's own rounding is **disabled**
//! (Go owns every rounding rule, in device pixels), and compute happens in
//! device pixels with `Definite` available-space values supplied by the
//! Go adapter.
//!
//! # Contract highlights
//!
//! * Every entry point contains panics with `catch_unwind` and returns a
//!   status; a panicking export marks the engine failed
//!   ([`layout_status::ERR_PANIC`], then [`layout_status::ERR_ENGINE_FAILED`]
//!   for subsequent calls until reset/dispose).
//! * Engine handles encode `(slot index << 20) | slot generation`; node
//!   handles encode `(engine node-generation << 44) | node sequence`. A node
//!   handle is stale after engine reset or subtree removal; an engine handle
//!   is stale after dispose/slot reuse. Stale use returns
//!   [`layout_status::ERR_STALE_HANDLE`].
//! * The engine is *busy* from compute entry until return. Every other
//!   engine operation checks the busy flag **before** taking the engine
//!   state lock, so a same-engine re-entry from inside a measure trampoline
//!   is rejected with [`layout_status::ERR_ENGINE_BUSY`] instead of
//!   deadlocking. Independent-engine nested compute is allowed.
//! * The measure trampoline is one process-global function pointer set via
//!   `layout_set_trampoline`. Measure requests and responses carry device
//!   pixels (Taffy's units); all logical-unit conversion and snapping is
//!   owned by the Go adapter.
//! * No Go pointers are retained: measure requests/responses are
//!   native-owned stack records, and the only callback identity crossing
//!   the ABI is the opaque `callback_token` u64.
//!
//! [`LAYOUT_ABI.md`]: ../LAYOUT_ABI.md

use std::collections::HashMap;
use std::panic::{catch_unwind, AssertUnwindSafe};
use std::sync::atomic::{AtomicBool, AtomicUsize, Ordering};
use std::sync::{Arc, Mutex, MutexGuard};

use taffy::geometry::{Line, Point as TaffyPoint, Rect as TaffyRect, Size as TaffySize};
use taffy::prelude::{max_content, min_content, TaffyGridLine, TaffyGridSpan};
use taffy::style::{
    AlignContent, AlignItems, AvailableSpace, Dimension, Display, FlexDirection, FlexWrap,
    LengthPercentage, LengthPercentageAuto, Overflow, Position, Style as TaffyStyle,
};
use taffy::style_helpers::{fr, length, minmax, repeat};
use taffy::tree::NodeId;
use taffy::TaffyTree;

/// Layout service ABI version (independent of the bootstrap table's
/// `abi_version`; bumped only on layout-record/table layout changes).
pub const GPUI_GO_LAYOUT_SERVICE_VERSION: u32 = 1;

/// Layout service status codes. 0 is success, negative values are
/// caller/argument errors, 100+ are internal failures.
pub mod layout_status {
    /// Success.
    pub const OK: i32 = 0;
    /// The handle encoded a past generation (engine disposed/reset before,
    /// node removed or engine reset since). See `LAYOUT_ABI.md`.
    pub const ERR_STALE_HANDLE: i32 = -1;
    /// The handle never existed or is malformed (out-of-range slot, node
    /// sequence never issued).
    pub const ERR_BAD_HANDLE: i32 = -2;
    /// A required pointer argument was null.
    pub const ERR_NULL_ARG: i32 = -3;
    /// A record field held an invalid tag, enumerant or bound (style record
    /// tags/enums, available-space tags, line/span/placement bounds,
    /// record-size self-check mismatch).
    pub const ERR_BAD_VALUE: i32 = -4;
    /// `layout_node_create` was given a child that is already attached to a
    /// parent.
    pub const ERR_NODE_ATTACHED: i32 = -5;
    /// The engine is busy computing; the operation was rejected before
    /// taking the engine state lock (nested same-engine re-entry, or a
    /// concurrent call while a compute runs).
    pub const ERR_ENGINE_BUSY: i32 = -6;
    /// A computed tree contains a measured node but no trampoline is
    /// registered.
    pub const ERR_NO_TRAMPOLINE: i32 = -7;
    /// `layout_dump` capacity was smaller than the subtree; `out_count`
    /// carries the required count.
    pub const ERR_CAPACITY: i32 = -8;
    /// The engine is marked failed (a panic or callback failure occurred);
    /// reset or dispose is required.
    pub const ERR_ENGINE_FAILED: i32 = 100;
    /// A panic was contained by `catch_unwind` during this call; the engine
    /// is now marked failed.
    pub const ERR_PANIC: i32 = 101;
    /// A measure trampoline call failed (callback panic or invalid token)
    /// during this compute; failure was latched, further callbacks were
    /// suppressed, and the engine is now marked failed.
    pub const ERR_CALLBACK: i32 = 102;
}

// ---------------------------------------------------------------------------
// Records
// ---------------------------------------------------------------------------

/// Length tag values shared by every `GpuiLen` field.
pub mod len_tag {
    /// `auto` (bits ignored).
    pub const AUTO: u32 = 0;
    /// Definite device pixels (Go already converted logical pixels with the
    /// pinned rounding rules).
    pub const DEFINITE: u32 = 1;
    /// Percentage (fraction of parent, f32 bits).
    pub const PERCENT: u32 = 2;
    /// Field omitted; the native side keeps the Taffy default.
    pub const ABSENT: u32 = 3;
    // Tags 4 (min-content) and 5 (max-content) are reserved by the ABI
    // document, but the pinned gpui `Length`/`DefiniteLength` enums have no
    // such keywords; this implementation rejects them as caller errors.
}

/// Length value: tag + value bits (see [`len_tag`]). 8 bytes, align 4.
#[repr(C)]
#[derive(Clone, Copy, PartialEq, Debug)]
pub struct GpuiLen {
    /// One of the [`len_tag`] values.
    pub tag: u32,
    /// IEEE-754 bits of the f32 value when the tag is definite/percent.
    pub bits: u32,
}

/// Four lengths in top/right/bottom/left order. 32 bytes, align 4.
#[repr(C)]
#[derive(Clone, Copy, PartialEq, Debug)]
pub struct GpuiEdges {
    pub top: GpuiLen,
    pub right: GpuiLen,
    pub bottom: GpuiLen,
    pub left: GpuiLen,
}

/// Two lengths (width, height). 16 bytes, align 4.
#[repr(C)]
#[derive(Clone, Copy, PartialEq, Debug)]
pub struct GpuiSizeL {
    pub width: GpuiLen,
    pub height: GpuiLen,
}

/// Available space on one axis: tag 0 = definite (bits), 1 = min-content,
/// 2 = max-content. 8 bytes, align 4.
#[repr(C)]
#[derive(Clone, Copy, PartialEq, Debug)]
pub struct GpuiAvail {
    pub tag: u32,
    pub bits: u32,
}

/// Available space on both axes. 16 bytes, align 4.
#[repr(C)]
#[derive(Clone, Copy, PartialEq, Debug)]
pub struct GpuiAvailSize {
    pub width: GpuiAvail,
    pub height: GpuiAvail,
}

/// Measurement request. Native-owned stack record handed to the Go
/// trampoline; **all definite values are device pixels**.
///
/// Layout (x86-64, `#[repr(C)]`): `engine` @0, `node` @8, `callback_token`
/// @16, `compute_token` @24, `known_width_present` @32,
/// `known_height_present` @36, `avail_width_tag` @40, `avail_height_tag` @44,
/// `known_width_bits` @48, `known_height_bits` @52, `avail_width_bits` @56,
/// `avail_height_bits` @60, `reserved` @64; size 72, align 8.
#[repr(C)]
#[derive(Clone, Copy)]
pub struct GpuiMeasureRequest {
    /// Engine handle the compute runs on.
    pub engine: u64,
    /// Public node handle of the measured node.
    pub node: u64,
    /// Opaque token registered by the Go side with the node.
    pub callback_token: u64,
    /// Token passed to `layout_compute` for this compute.
    pub compute_token: u64,
    /// 0/1: whether the width is a known (fixed) dimension.
    pub known_width_present: u32,
    /// 0/1: whether the height is a known (fixed) dimension.
    pub known_height_present: u32,
    /// [`GpuiAvail`] tag for the width axis.
    pub avail_width_tag: u32,
    /// [`GpuiAvail`] tag for the height axis.
    pub avail_height_tag: u32,
    /// Width bits when known.
    pub known_width_bits: u32,
    /// Height bits when known.
    pub known_height_bits: u32,
    /// Definite width bits when the tag is 0.
    pub avail_width_bits: u32,
    /// Definite height bits when the tag is 0.
    pub avail_height_bits: u32,
    /// Reserved; must be zero.
    pub reserved: [u32; 2],
}

/// Measurement response written in place by the Go trampoline.
///
/// Layout: `status` @0, `width_bits` @4, `height_bits` @8; size 12, align 4.
/// `status`: 0 ok, 1 callback failed (panic recovered), 2 invalid token.
/// `width_bits`/`height_bits` are device pixels (Taffy's units; the Go
/// adapter applied the pinned measurement snapping before writing them).
#[repr(C)]
#[derive(Clone, Copy)]
pub struct GpuiMeasureResponse {
    pub status: i32,
    pub width_bits: u32,
    pub height_bits: u32,
}

/// Computed layout for one node, mirroring the Taffy `Layout` fields the
/// GPUI adapter consumes. All values are unrounded device pixels (Taffy
/// rounding is disabled).
///
/// Layout: `order` @0, `location_x` @4, `location_y` @8, `size_w` @12,
/// `size_h` @16, `content_w` @20, `content_h` @24, `scrollbar_w` @28,
/// `scrollbar_h` @32, `border_top` @36, `border_right` @40, `border_bottom`
/// @44, `border_left` @48, `padding_top` @52, `padding_right` @56,
/// `padding_bottom` @60, `padding_left` @64, `record_size` @68;
/// size 72, align 4.
#[repr(C)]
#[derive(Clone, Copy, PartialEq, Debug)]
pub struct GpuiGoLayoutRecord {
    /// Relative ordering of the node (topological paint order).
    pub order: i32,
    /// Top-left corner relative to the parent.
    pub location_x: f32,
    pub location_y: f32,
    /// Border-box size.
    pub size_w: f32,
    pub size_h: f32,
    /// Content size (may overflow `size`).
    pub content_w: f32,
    pub content_h: f32,
    /// Scrollbar size.
    pub scrollbar_w: f32,
    pub scrollbar_h: f32,
    /// Border widths (pre-layout snapped device pixels).
    pub border_top: f32,
    pub border_right: f32,
    pub border_bottom: f32,
    pub border_left: f32,
    /// Padding (pre-layout snapped device pixels).
    pub padding_top: f32,
    pub padding_right: f32,
    pub padding_bottom: f32,
    pub padding_left: f32,
    /// `size_of::<GpuiGoLayoutRecord>()` self-check.
    pub record_size: u32,
}

/// The GPUI-forwarded style set, mirroring what the pinned
/// `crates/gpui/src/taffy.rs::into_taffy_style` forwards. Every length the
/// Go adapter writes is already device pixels (or a percent fraction);
/// `tag 3` (absent) leaves the Taffy default in place.
///
/// All fields are `u32`-sized: total 336 bytes, alignment 4. Field order and
/// offsets are part of the ABI; the Go mirror asserts
/// `unsafe.Sizeof` against the service table's `size_of_style_record`.
#[repr(C)]
#[derive(Clone, Copy, Debug)]
pub struct GpuiGoLayoutStyleRecord {
    // --- display / position / overflow -------------------------------------
    /// 0 flex, 1 block, 2 grid, 3 none (Inline→block, InlineFlex→flex are
    /// mapped by the Go adapter before the record).
    pub display: u32,
    /// 0 relative, 1 absolute.
    pub position: u32,
    /// 0 visible, 1 clip, 2 hidden, 3 scroll (gpui enum source order).
    pub overflow_x: u32,
    /// 0 visible, 1 clip, 2 hidden, 3 scroll.
    pub overflow_y: u32,
    /// Scrollbar width: tag 1 (device px) or 3 (absent); tags 0/2 are errors
    /// (gpui's `AbsoluteLength` has no auto/percent).
    pub scrollbar_width: GpuiLen,

    // --- insets and sizing --------------------------------------------------
    /// Inset per edge (`Length`: auto/definite/percent/absent).
    pub inset: GpuiEdges,
    /// Size per axis (`Length`).
    pub size: GpuiSizeL,
    /// Minimum size per axis (`Length`).
    pub min_size: GpuiSizeL,
    /// Maximum size per axis (`Length`).
    pub max_size: GpuiSizeL,
    /// 0/1 aspect-ratio presence flag.
    pub aspect_ratio_present: u32,
    /// Aspect-ratio f32 bits (width / height).
    pub aspect_ratio_bits: u32,

    // --- spacing -------------------------------------------------------------
    /// Margin per edge (`Length`).
    pub margin: GpuiEdges,
    /// Padding per edge (`DefiniteLength`: device px, percent or absent;
    /// auto is an error).
    pub padding: GpuiEdges,
    /// Border widths, device px f32 bits, already stroke-snapped by the Go
    /// adapter (pinned `round_stroke_to_device_pixel`).
    pub border_top: u32,
    /// Border width, device px bits.
    pub border_right: u32,
    /// Border width, device px bits.
    pub border_bottom: u32,
    /// Border width, device px bits.
    pub border_left: u32,

    // --- alignment -----------------------------------------------------------
    /// 0/1 presence + value: 0 start, 1 end, 2 flex-start, 3 flex-end,
    /// 4 center, 5 baseline, 6 stretch (gpui `AlignItems` source order).
    pub align_items_present: u32,
    /// `AlignItems` value.
    pub align_items: u32,
    /// 0/1 presence; same value mapping as `align_items` (`AlignSelf`).
    pub align_self_present: u32,
    /// `AlignSelf` value.
    pub align_self: u32,
    /// 0/1 presence + value: 0 start, 1 end, 2 flex-start, 3 flex-end,
    /// 4 center, 5 stretch, 6 space-between, 7 space-evenly, 8 space-around
    /// (gpui `AlignContent` source order).
    pub align_content_present: u32,
    /// `AlignContent` value.
    pub align_content: u32,
    /// 0/1 presence; same value mapping as `align_content` (`JustifyContent`).
    pub justify_content_present: u32,
    /// `JustifyContent` value.
    pub justify_content: u32,
    /// Gap per axis (`DefiniteLength`: device px, percent or absent).
    pub gap: GpuiSizeL,

    // --- flexbox -------------------------------------------------------------
    /// 0 row, 1 column, 2 row-reverse, 3 column-reverse.
    pub flex_direction: u32,
    /// 0 no-wrap, 1 wrap, 2 wrap-reverse.
    pub flex_wrap: u32,
    /// Flex basis (`Length`).
    pub flex_basis: GpuiLen,
    /// 0/1 presence flag for `flex_grow`.
    pub flex_grow_present: u32,
    /// `flex_grow` f32 bits.
    pub flex_grow_bits: u32,
    /// 0/1 presence flag for `flex_shrink`.
    pub flex_shrink_present: u32,
    /// `flex_shrink` f32 bits.
    pub flex_shrink_bits: u32,

    // --- grid ----------------------------------------------------------------
    /// 0/1: whether `grid_rows` is set (gpui `Option<GridTemplate>`).
    pub grid_template_rows_present: u32,
    /// Repeat count of the template (0..=65535, gpui `u16`).
    pub grid_template_rows_repeat: u32,
    /// 0 zero, 1 min-content, 2 max-content (gpui `GridTemplateMinSize`).
    pub grid_template_rows_min_size: u32,
    /// 0/1: whether `grid_cols` is set.
    pub grid_template_columns_present: u32,
    /// Repeat count of the column template (0..=65535).
    pub grid_template_columns_repeat: u32,
    /// 0 zero, 1 min-content, 2 max-content.
    pub grid_template_columns_min_size: u32,
    /// 0/1: whether `grid_location` is set (gpui `Option<GridLocation>`).
    pub grid_placement_present: u32,
    /// Row start placement: 0 auto, 1 line, 2 span.
    pub grid_row_start_kind: u32,
    /// Row start value: line index (i16 bit pattern) or span (u16).
    pub grid_row_start_value: u32,
    /// Row end placement kind.
    pub grid_row_end_kind: u32,
    /// Row end value.
    pub grid_row_end_value: u32,
    /// Column start placement kind.
    pub grid_column_start_kind: u32,
    /// Column start value.
    pub grid_column_start_value: u32,
    /// Column end placement kind.
    pub grid_column_end_kind: u32,
    /// Column end value.
    pub grid_column_end_value: u32,

    /// `size_of::<GpuiGoLayoutStyleRecord>()` self-check; a mismatch is a
    /// caller error before anything else is read.
    pub record_size: u32,
}

// Compile-time layout pins; the Go loader mirrors these exactly.
const _: () = assert!(std::mem::size_of::<GpuiLen>() == 8);
const _: () = assert!(std::mem::align_of::<GpuiLen>() == 4);
const _: () = assert!(std::mem::size_of::<GpuiEdges>() == 32);
const _: () = assert!(std::mem::size_of::<GpuiSizeL>() == 16);
const _: () = assert!(std::mem::size_of::<GpuiAvail>() == 8);
const _: () = assert!(std::mem::size_of::<GpuiAvailSize>() == 16);
const _: () = assert!(std::mem::size_of::<GpuiMeasureRequest>() == 72);
const _: () = assert!(std::mem::align_of::<GpuiMeasureRequest>() == 8);
const _: () = assert!(std::mem::size_of::<GpuiMeasureResponse>() == 12);
const _: () = assert!(std::mem::align_of::<GpuiMeasureResponse>() == 4);
const _: () = assert!(std::mem::size_of::<GpuiGoLayoutRecord>() == 72);
const _: () = assert!(std::mem::align_of::<GpuiGoLayoutRecord>() == 4);
const _: () = assert!(std::mem::size_of::<GpuiGoLayoutStyleRecord>() == 336);
const _: () = assert!(std::mem::align_of::<GpuiGoLayoutStyleRecord>() == 4);
const _: () = assert!(std::mem::size_of::<GpuiGoLayoutTable>() == 160);
const _: () = assert!(std::mem::align_of::<GpuiGoLayoutTable>() == 8);

// ---------------------------------------------------------------------------
// Handle packing
// ---------------------------------------------------------------------------

/// Engine handle: `(slot index << 20) | slot generation`, slot generation
/// in the low 20 bits.
const ENGINE_HANDLE_SLOT_SHIFT: u32 = 20;
/// Maximum slot generation (20 bits).
const ENGINE_GENERATION_MAX: u32 = 0x000F_FFFF;
/// Node handle: `(node generation << 44) | node sequence`.
const NODE_HANDLE_GENERATION_SHIFT: u32 = 44;
/// Maximum per-engine node generation (20 bits, the handle's top bits).
const NODE_GENERATION_MAX: u32 = 0x000F_FFFF;
/// Mask of the node-sequence bits of a node handle.
const NODE_SEQUENCE_MASK: u64 = 0x0000_0FFF_FFFF_FFFF;

fn engine_handle_parts(handle: u64) -> (usize, u32) {
    (
        (handle >> ENGINE_HANDLE_SLOT_SHIFT) as usize,
        (handle & ENGINE_GENERATION_MAX as u64) as u32,
    )
}

fn make_engine_handle(slot: usize, generation: u32) -> u64 {
    ((slot as u64) << ENGINE_HANDLE_SLOT_SHIFT) | generation as u64
}

fn make_node_handle(generation: u32, sequence: u64) -> u64 {
    ((generation as u64) << NODE_HANDLE_GENERATION_SHIFT) | sequence
}

fn node_handle_parts(handle: u64) -> (u32, u64) {
    (
        (handle >> NODE_HANDLE_GENERATION_SHIFT) as u32,
        handle & NODE_SEQUENCE_MASK,
    )
}

// ---------------------------------------------------------------------------
// Style translation (pinned into_taffy_style)
// ---------------------------------------------------------------------------

fn definite_bits(bits: u32) -> f32 {
    f32::from_bits(bits)
}

/// `GpuiLen` -> optional `LengthPercentageAuto` (inset/margin).
fn len_to_lpa(len: GpuiLen) -> Result<Option<LengthPercentageAuto>, i32> {
    match len.tag {
        len_tag::AUTO => Ok(Some(LengthPercentageAuto::auto())),
        len_tag::DEFINITE => Ok(Some(LengthPercentageAuto::length(definite_bits(len.bits)))),
        len_tag::PERCENT => Ok(Some(LengthPercentageAuto::percent(definite_bits(len.bits)))),
        len_tag::ABSENT => Ok(None),
        _ => Err(layout_status::ERR_BAD_VALUE),
    }
}

/// `GpuiLen` -> optional `LengthPercentage` (padding/gap; no auto).
fn len_to_lp(len: GpuiLen) -> Result<Option<LengthPercentage>, i32> {
    match len.tag {
        len_tag::DEFINITE => Ok(Some(LengthPercentage::length(definite_bits(len.bits)))),
        len_tag::PERCENT => Ok(Some(LengthPercentage::percent(definite_bits(len.bits)))),
        len_tag::ABSENT => Ok(None),
        _ => Err(layout_status::ERR_BAD_VALUE),
    }
}

/// `GpuiLen` -> optional `Dimension` (size/min/max/basis).
fn len_to_dimension(len: GpuiLen) -> Result<Option<Dimension>, i32> {
    match len.tag {
        len_tag::AUTO => Ok(Some(Dimension::auto())),
        len_tag::DEFINITE => Ok(Some(Dimension::length(definite_bits(len.bits)))),
        len_tag::PERCENT => Ok(Some(Dimension::percent(definite_bits(len.bits)))),
        len_tag::ABSENT => Ok(None),
        _ => Err(layout_status::ERR_BAD_VALUE),
    }
}

fn display_from(value: u32) -> Result<Display, i32> {
    match value {
        0 => Ok(Display::Flex),
        1 => Ok(Display::Block),
        2 => Ok(Display::Grid),
        3 => Ok(Display::None),
        _ => Err(layout_status::ERR_BAD_VALUE),
    }
}

fn position_from(value: u32) -> Result<Position, i32> {
    match value {
        0 => Ok(Position::Relative),
        1 => Ok(Position::Absolute),
        _ => Err(layout_status::ERR_BAD_VALUE),
    }
}

fn overflow_from(value: u32) -> Result<Overflow, i32> {
    // gpui `Overflow` source order: Visible, Clip, Hidden, Scroll.
    match value {
        0 => Ok(Overflow::Visible),
        1 => Ok(Overflow::Clip),
        2 => Ok(Overflow::Hidden),
        3 => Ok(Overflow::Scroll),
        _ => Err(layout_status::ERR_BAD_VALUE),
    }
}

fn flex_direction_from(value: u32) -> Result<FlexDirection, i32> {
    // gpui `FlexDirection` source order: Row, Column, RowReverse,
    // ColumnReverse.
    match value {
        0 => Ok(FlexDirection::Row),
        1 => Ok(FlexDirection::Column),
        2 => Ok(FlexDirection::RowReverse),
        3 => Ok(FlexDirection::ColumnReverse),
        _ => Err(layout_status::ERR_BAD_VALUE),
    }
}

fn flex_wrap_from(value: u32) -> Result<FlexWrap, i32> {
    // gpui `FlexWrap` source order: NoWrap, Wrap, WrapReverse.
    match value {
        0 => Ok(FlexWrap::NoWrap),
        1 => Ok(FlexWrap::Wrap),
        2 => Ok(FlexWrap::WrapReverse),
        _ => Err(layout_status::ERR_BAD_VALUE),
    }
}

fn align_items_from(value: u32) -> Result<AlignItems, i32> {
    // gpui `AlignItems` source order: Start, End, FlexStart, FlexEnd,
    // Center, Baseline, Stretch.
    match value {
        0 => Ok(AlignItems::START),
        1 => Ok(AlignItems::END),
        2 => Ok(AlignItems::FLEX_START),
        3 => Ok(AlignItems::FLEX_END),
        4 => Ok(AlignItems::CENTER),
        5 => Ok(AlignItems::BASELINE),
        6 => Ok(AlignItems::STRETCH),
        _ => Err(layout_status::ERR_BAD_VALUE),
    }
}

fn align_content_from(value: u32) -> Result<AlignContent, i32> {
    // gpui `AlignContent` source order: Start, End, FlexStart, FlexEnd,
    // Center, Stretch, SpaceBetween, SpaceEvenly, SpaceAround.
    match value {
        0 => Ok(AlignContent::START),
        1 => Ok(AlignContent::END),
        2 => Ok(AlignContent::FLEX_START),
        3 => Ok(AlignContent::FLEX_END),
        4 => Ok(AlignContent::CENTER),
        5 => Ok(AlignContent::STRETCH),
        6 => Ok(AlignContent::SPACE_BETWEEN),
        7 => Ok(AlignContent::SPACE_EVENLY),
        8 => Ok(AlignContent::SPACE_AROUND),
        _ => Err(layout_status::ERR_BAD_VALUE),
    }
}

fn present_flag(value: u32) -> Result<bool, i32> {
    match value {
        0 => Ok(false),
        1 => Ok(true),
        _ => Err(layout_status::ERR_BAD_VALUE),
    }
}

/// Grid placement: 0 auto, 1 line (i16 bit pattern), 2 span (u16).
fn grid_placement_from(kind: u32, value: u32) -> Result<taffy::style::GridPlacement, i32> {
    match kind {
        0 => Ok(taffy::style::GridPlacement::Auto),
        1 => {
            if value > u16::MAX as u32 {
                return Err(layout_status::ERR_BAD_VALUE);
            }
            Ok(taffy::style::GridPlacement::from_line_index(value as u16 as i16))
        }
        2 => {
            if value > u16::MAX as u32 {
                return Err(layout_status::ERR_BAD_VALUE);
            }
            Ok(taffy::style::GridPlacement::from_span(value as u16))
        }
        _ => Err(layout_status::ERR_BAD_VALUE),
    }
}

/// One gpui `Option<GridTemplate>` (present, repeat, min size) -> the Taffy
/// template vector, exactly as the pinned `to_grid_repeat` does:
/// `repeat(<count>, minmax(<min>, 1fr))`.
fn grid_template_from<S: taffy::CheapCloneStr>(
    present: u32,
    repeat_count: u32,
    min_size: u32,
) -> Result<Vec<taffy::style::GridTemplateComponent<S>>, i32> {
    if !present_flag(present)? {
        return Ok(Vec::new());
    }
    let repeat_count = u16::try_from(repeat_count).map_err(|_| layout_status::ERR_BAD_VALUE)?;
    let min = match min_size {
        0 => length(0.0_f32),
        1 => min_content(),
        2 => max_content(),
        _ => return Err(layout_status::ERR_BAD_VALUE),
    };
    Ok(vec![repeat(repeat_count, vec![minmax(min, fr(1.0_f32))])])
}

/// Translates a validated style record into a Taffy style, mirroring the
/// pinned `crates/gpui/src/taffy.rs::into_taffy_style`. Absent fields keep
/// the Taffy default; every tag/enumerant/bound is validated first.
pub(crate) fn into_taffy_style(rec: &GpuiGoLayoutStyleRecord) -> Result<TaffyStyle, i32> {
    if rec.record_size != std::mem::size_of::<GpuiGoLayoutStyleRecord>() as u32 {
        return Err(layout_status::ERR_BAD_VALUE);
    }

    let mut style = TaffyStyle::default();

    style.display = display_from(rec.display)?;
    style.position = position_from(rec.position)?;
    style.overflow = TaffyPoint {
        x: overflow_from(rec.overflow_x)?,
        y: overflow_from(rec.overflow_y)?,
    };

    match rec.scrollbar_width.tag {
        len_tag::DEFINITE => style.scrollbar_width = definite_bits(rec.scrollbar_width.bits),
        len_tag::ABSENT => {}
        _ => return Err(layout_status::ERR_BAD_VALUE),
    }

    if let Some(v) = len_to_lpa(rec.inset.top)? {
        style.inset.top = v;
    }
    if let Some(v) = len_to_lpa(rec.inset.right)? {
        style.inset.right = v;
    }
    if let Some(v) = len_to_lpa(rec.inset.bottom)? {
        style.inset.bottom = v;
    }
    if let Some(v) = len_to_lpa(rec.inset.left)? {
        style.inset.left = v;
    }

    if let Some(v) = len_to_dimension(rec.size.width)? {
        style.size.width = v;
    }
    if let Some(v) = len_to_dimension(rec.size.height)? {
        style.size.height = v;
    }
    if let Some(v) = len_to_dimension(rec.min_size.width)? {
        style.min_size.width = v;
    }
    if let Some(v) = len_to_dimension(rec.min_size.height)? {
        style.min_size.height = v;
    }
    if let Some(v) = len_to_dimension(rec.max_size.width)? {
        style.max_size.width = v;
    }
    if let Some(v) = len_to_dimension(rec.max_size.height)? {
        style.max_size.height = v;
    }

    if present_flag(rec.aspect_ratio_present)? {
        style.aspect_ratio = Some(definite_bits(rec.aspect_ratio_bits));
    }

    if let Some(v) = len_to_lpa(rec.margin.top)? {
        style.margin.top = v;
    }
    if let Some(v) = len_to_lpa(rec.margin.right)? {
        style.margin.right = v;
    }
    if let Some(v) = len_to_lpa(rec.margin.bottom)? {
        style.margin.bottom = v;
    }
    if let Some(v) = len_to_lpa(rec.margin.left)? {
        style.margin.left = v;
    }

    if let Some(v) = len_to_lp(rec.padding.top)? {
        style.padding.top = v;
    }
    if let Some(v) = len_to_lp(rec.padding.right)? {
        style.padding.right = v;
    }
    if let Some(v) = len_to_lp(rec.padding.bottom)? {
        style.padding.bottom = v;
    }
    if let Some(v) = len_to_lp(rec.padding.left)? {
        style.padding.left = v;
    }

    // Borders are always present (gpui's Edges<AbsoluteLength> defaults to
    // zero) and already stroke-snapped by the Go adapter.
    style.border = TaffyRect {
        top: LengthPercentage::length(definite_bits(rec.border_top)),
        right: LengthPercentage::length(definite_bits(rec.border_right)),
        bottom: LengthPercentage::length(definite_bits(rec.border_bottom)),
        left: LengthPercentage::length(definite_bits(rec.border_left)),
    };

    if present_flag(rec.align_items_present)? {
        style.align_items = Some(align_items_from(rec.align_items)?);
    }
    if present_flag(rec.align_self_present)? {
        style.align_self = Some(align_items_from(rec.align_self)?);
    }
    if present_flag(rec.align_content_present)? {
        style.align_content = Some(align_content_from(rec.align_content)?);
    }
    if present_flag(rec.justify_content_present)? {
        style.justify_content = Some(align_content_from(rec.justify_content)?);
    }

    if let Some(v) = len_to_lp(rec.gap.width)? {
        style.gap.width = v;
    }
    if let Some(v) = len_to_lp(rec.gap.height)? {
        style.gap.height = v;
    }

    style.flex_direction = flex_direction_from(rec.flex_direction)?;
    style.flex_wrap = flex_wrap_from(rec.flex_wrap)?;

    if let Some(v) = len_to_dimension(rec.flex_basis)? {
        style.flex_basis = v;
    }
    if present_flag(rec.flex_grow_present)? {
        style.flex_grow = definite_bits(rec.flex_grow_bits);
    }
    if present_flag(rec.flex_shrink_present)? {
        style.flex_shrink = definite_bits(rec.flex_shrink_bits);
    }

    style.grid_template_rows = grid_template_from(
        rec.grid_template_rows_present,
        rec.grid_template_rows_repeat,
        rec.grid_template_rows_min_size,
    )?;
    style.grid_template_columns = grid_template_from(
        rec.grid_template_columns_present,
        rec.grid_template_columns_repeat,
        rec.grid_template_columns_min_size,
    )?;

    if present_flag(rec.grid_placement_present)? {
        style.grid_row = Line {
            start: grid_placement_from(rec.grid_row_start_kind, rec.grid_row_start_value)?,
            end: grid_placement_from(rec.grid_row_end_kind, rec.grid_row_end_value)?,
        };
        style.grid_column = Line {
            start: grid_placement_from(rec.grid_column_start_kind, rec.grid_column_start_value)?,
            end: grid_placement_from(rec.grid_column_end_kind, rec.grid_column_end_value)?,
        };
    }

    Ok(style)
}

fn avail_from(avail: GpuiAvail) -> Result<AvailableSpace, i32> {
    match avail.tag {
        0 => Ok(AvailableSpace::Definite(definite_bits(avail.bits))),
        1 => Ok(AvailableSpace::MinContent),
        2 => Ok(AvailableSpace::MaxContent),
        _ => Err(layout_status::ERR_BAD_VALUE),
    }
}

fn avail_parts(avail: &AvailableSpace) -> (u32, u32) {
    match avail {
        AvailableSpace::Definite(value) => (0, value.to_bits()),
        AvailableSpace::MinContent => (1, 0),
        AvailableSpace::MaxContent => (2, 0),
    }
}

// ---------------------------------------------------------------------------
// Engine registry
// ---------------------------------------------------------------------------

/// Per-node measure context: the data Taffy stores for measured leaves.
/// This is the native side of the "measure closure map" — the public
/// callback identity is the opaque `callback_token` assigned by Go.
#[derive(Clone, Copy, Debug)]
struct MeasureContext {
    /// Public node handle (encoded for the current generation).
    node_handle: u64,
    /// Opaque token the Go trampoline resolves through its registry.
    callback_token: u64,
}

struct EngineState {
    /// Current node-generation (embedded in every node handle; bumped on
    /// reset, invalidating all existing node handles).
    node_generation: u32,
    taffy: TaffyTree<MeasureContext>,
    /// node sequence -> taffy node.
    nodes: HashMap<u64, NodeId>,
    /// taffy node -> node sequence.
    reverse: HashMap<NodeId, u64>,
    /// Next node sequence to hand out (monotonic within a generation).
    next_node_seq: u64,
    /// Failed flag: set by contained panics and latched callback failures;
    /// cleared only by reset.
    failed: bool,
}

// SAFETY: `TaffyTree` is `!Send` only because `Style`'s compact length can
// carry a `*const ()` calc-tree pointer under the `calc` feature. Every
// style stored in this engine is built by `into_taffy_style`, which never
// produces calc values, so that pointer is never populated; the rest of the
// tree is plain owned data with no thread affinity. The Go caller may hop
// OS threads between calls (goroutine migration), so the engine state must
// be sendable.
unsafe impl Send for EngineState {}

impl EngineState {
    fn new() -> Self {
        let mut taffy = TaffyTree::new();
        // The pinned gpui engine disables Taffy rounding: all snapping is
        // done by the Go adapter in device pixels.
        taffy.disable_rounding();
        EngineState {
            node_generation: 1,
            taffy,
            nodes: HashMap::new(),
            reverse: HashMap::new(),
            next_node_seq: 0,
            failed: false,
        }
    }

    fn alloc_handle(&mut self, node: NodeId) -> Result<u64, i32> {
        let seq = self.next_node_seq;
        if seq > NODE_SEQUENCE_MASK {
            return Err(layout_status::ERR_BAD_VALUE);
        }
        self.next_node_seq += 1;
        let handle = make_node_handle(self.node_generation, seq);
        self.nodes.insert(seq, node);
        self.reverse.insert(node, seq);
        Ok(handle)
    }

    fn resolve_node(&self, handle: u64) -> Result<NodeId, i32> {
        let (generation, sequence) = node_handle_parts(handle);
        if generation != self.node_generation {
            return Err(layout_status::ERR_STALE_HANDLE);
        }
        if sequence >= self.next_node_seq {
            return Err(layout_status::ERR_BAD_HANDLE);
        }
        match self.nodes.get(&sequence) {
            Some(&node) => Ok(node),
            None => Err(layout_status::ERR_STALE_HANDLE),
        }
    }

    fn handle_of(&self, node: NodeId) -> Result<u64, i32> {
        match self.reverse.get(&node) {
            Some(&sequence) => Ok(make_node_handle(self.node_generation, sequence)),
            None => Err(layout_status::ERR_BAD_HANDLE),
        }
    }
}

/// The engine shared between the registry and in-flight calls. `busy` is
/// checked *before* taking `state`, so re-entrant same-engine calls are
/// rejected instead of deadlocking on the state mutex.
struct EngineShared {
    busy: AtomicBool,
    state: Mutex<EngineState>,
}

struct EngineSlot {
    /// Slot generation of the last engine that occupied this slot (persists
    /// while vacant so disposed handles stay stale after slot reuse).
    slot_generation: u32,
    occupied: bool,
    engine: Option<Arc<EngineShared>>,
}

impl EngineSlot {
    fn vacant() -> Self {
        EngineSlot { slot_generation: 0, occupied: false, engine: None }
    }
}

static REGISTRY: Mutex<Vec<EngineSlot>> = Mutex::new(Vec::new());

/// Process-global trampoline (raw address; 0 = unregistered).
static TRAMPOLINE: AtomicUsize = AtomicUsize::new(0);

/// The Go measure trampoline signature.
pub type GpuiGoLayoutTrampoline =
    unsafe extern "system" fn(*mut GpuiMeasureRequest, *mut GpuiMeasureResponse) -> i32;

fn registry_lock() -> MutexGuard<'static, Vec<EngineSlot>> {
    REGISTRY.lock().unwrap_or_else(|poisoned| poisoned.into_inner())
}

fn lock_state(shared: &EngineShared) -> MutexGuard<'_, EngineState> {
    shared.state.lock().unwrap_or_else(|poisoned| poisoned.into_inner())
}

/// Resolves an engine handle to its shared state. Never holds the registry
/// lock past this function: in-flight compute (which owns the engine state
/// lock) may call back into the registry for a *different* engine.
fn resolve_engine(handle: u64) -> Result<Arc<EngineShared>, i32> {
    let (slot_index, slot_generation) = engine_handle_parts(handle);
    let registry = registry_lock();
    if slot_index >= registry.len() {
        return Err(layout_status::ERR_BAD_HANDLE);
    }
    let slot = &registry[slot_index];
    if !slot.occupied || slot.slot_generation != slot_generation {
        return Err(layout_status::ERR_STALE_HANDLE);
    }
    slot.engine
        .clone()
        .ok_or(layout_status::ERR_STALE_HANDLE)
}

/// Marks an engine failed after a contained panic. Best-effort: a
/// concurrently disposed engine is ignored.
fn mark_engine_failed(handle: u64) {
    let shared = match resolve_engine(handle) {
        Ok(shared) => shared,
        Err(_) => return,
    };
    lock_state(&shared).failed = true;
}

/// Resets `busy` when the compute scope ends, including during unwinding.
struct BusyGuard(Arc<EngineShared>);

impl Drop for BusyGuard {
    fn drop(&mut self) {
        self.0.busy.store(false, Ordering::SeqCst);
    }
}

/// Why a compute failed after Taffy returned.
#[derive(Clone, Copy, PartialEq, Eq, Debug)]
enum ComputeFailure {
    /// A measured node was reached while no trampoline is registered.
    MissingTrampoline,
    /// The trampoline (or the Go measure function behind it) failed; the
    /// failure is latched for the rest of this compute.
    CallbackFailed,
}

// ---------------------------------------------------------------------------
// Service table
// ---------------------------------------------------------------------------

/// The layout service table installed in reserved slot 1 of
/// `GpuiGoAbiTable`. All 14 function pointers come first (112 bytes), then
/// 11 self-check scalars (44 bytes). Offsets (x86-64): pointers @0..112,
/// `service_version` @112, `size_of_table` @116, `align_of_table` @120,
/// `size_of_style_record` @124, `size_of_layout_record` @128,
/// `size_of_avail` @132, `size_of_avail_size` @136,
/// `size_of_measure_request` @140, `align_of_measure_request` @144,
/// `size_of_measure_response` @148, `align_of_measure_response` @152;
/// total size 160 (4 bytes of tail padding), alignment 8.
#[repr(C)]
pub struct GpuiGoLayoutTable {
    /// `layout_engine_create`.
    pub engine_create: Option<unsafe extern "system" fn(*mut u64) -> i32>,
    /// `layout_engine_reset`.
    pub engine_reset: Option<unsafe extern "system" fn(u64) -> i32>,
    /// `layout_engine_dispose`.
    pub engine_dispose: Option<unsafe extern "system" fn(u64) -> i32>,
    /// `layout_node_create`.
    pub node_create: Option<
        unsafe extern "system" fn(u64, *const GpuiGoLayoutStyleRecord, *const u64, u32, *mut u64) -> i32,
    >,
    /// `layout_node_set_style`.
    pub node_set_style: Option<unsafe extern "system" fn(u64, u64, *const GpuiGoLayoutStyleRecord) -> i32>,
    /// `layout_node_set_measure`.
    pub node_set_measure: Option<unsafe extern "system" fn(u64, u64, u64) -> i32>,
    /// `layout_node_remove_subtree`.
    pub node_remove_subtree: Option<unsafe extern "system" fn(u64, u64) -> i32>,
    /// `layout_compute`.
    pub compute: Option<unsafe extern "system" fn(u64, u64, *const GpuiAvailSize, u64, *mut u32) -> i32>,
    /// `layout_node_layout`.
    pub node_layout: Option<unsafe extern "system" fn(u64, u64, *mut GpuiGoLayoutRecord) -> i32>,
    /// `layout_node_parent`.
    pub node_parent: Option<unsafe extern "system" fn(u64, u64, *mut u64) -> i32>,
    /// `layout_node_child_count`.
    pub node_child_count: Option<unsafe extern "system" fn(u64, u64, *mut u32) -> i32>,
    /// `layout_node_child`.
    pub node_child: Option<unsafe extern "system" fn(u64, u64, u32, *mut u64) -> i32>,
    /// `layout_dump`.
    pub dump: Option<
        unsafe extern "system" fn(u64, u64, *mut u64, *mut GpuiGoLayoutRecord, u32, *mut u32) -> i32,
    >,
    /// `layout_set_trampoline`.
    pub set_trampoline: Option<unsafe extern "system" fn(GpuiGoLayoutTrampoline) -> i32>,
    /// Layout ABI version (see [`GPUI_GO_LAYOUT_SERVICE_VERSION`]).
    pub service_version: u32,
    /// `size_of::<GpuiGoLayoutTable>()` self-check.
    pub size_of_table: u32,
    /// `align_of::<GpuiGoLayoutTable>()` self-check.
    pub align_of_table: u32,
    /// `size_of::<GpuiGoLayoutStyleRecord>()` self-check.
    pub size_of_style_record: u32,
    /// `size_of::<GpuiGoLayoutRecord>()` self-check.
    pub size_of_layout_record: u32,
    /// `size_of::<GpuiAvail>()` self-check.
    pub size_of_avail: u32,
    /// `size_of::<GpuiAvailSize>()` self-check.
    pub size_of_avail_size: u32,
    /// `size_of::<GpuiMeasureRequest>()` self-check.
    pub size_of_measure_request: u32,
    /// `align_of::<GpuiMeasureRequest>()` self-check.
    pub align_of_measure_request: u32,
    /// `size_of::<GpuiMeasureResponse>()` self-check.
    pub size_of_measure_response: u32,
    /// `align_of::<GpuiMeasureResponse>()` self-check.
    pub align_of_measure_response: u32,
}

pub(crate) static LAYOUT_TABLE: GpuiGoLayoutTable = GpuiGoLayoutTable {
    engine_create: Some(layout_engine_create),
    engine_reset: Some(layout_engine_reset),
    engine_dispose: Some(layout_engine_dispose),
    node_create: Some(layout_node_create),
    node_set_style: Some(layout_node_set_style),
    node_set_measure: Some(layout_node_set_measure),
    node_remove_subtree: Some(layout_node_remove_subtree),
    compute: Some(layout_compute),
    node_layout: Some(layout_node_layout),
    node_parent: Some(layout_node_parent),
    node_child_count: Some(layout_node_child_count),
    node_child: Some(layout_node_child),
    dump: Some(layout_dump),
    set_trampoline: Some(layout_set_trampoline),
    service_version: GPUI_GO_LAYOUT_SERVICE_VERSION,
    size_of_table: std::mem::size_of::<GpuiGoLayoutTable>() as u32,
    align_of_table: std::mem::align_of::<GpuiGoLayoutTable>() as u32,
    size_of_style_record: std::mem::size_of::<GpuiGoLayoutStyleRecord>() as u32,
    size_of_layout_record: std::mem::size_of::<GpuiGoLayoutRecord>() as u32,
    size_of_avail: std::mem::size_of::<GpuiAvail>() as u32,
    size_of_avail_size: std::mem::size_of::<GpuiAvailSize>() as u32,
    size_of_measure_request: std::mem::size_of::<GpuiMeasureRequest>() as u32,
    align_of_measure_request: std::mem::align_of::<GpuiMeasureRequest>() as u32,
    size_of_measure_response: std::mem::size_of::<GpuiMeasureResponse>() as u32,
    align_of_measure_response: std::mem::align_of::<GpuiMeasureResponse>() as u32,
};

// ---------------------------------------------------------------------------
// Export bodies
// ---------------------------------------------------------------------------

fn engine_create_body(out_engine: *mut u64) -> i32 {
    if out_engine.is_null() {
        return layout_status::ERR_NULL_ARG;
    }
    let mut registry = registry_lock();
    let slot_index = match registry.iter().position(|slot| !slot.occupied) {
        Some(index) => index,
        None => {
            registry.push(EngineSlot::vacant());
            registry.len() - 1
        }
    };
    let slot_generation = match registry[slot_index].slot_generation.checked_add(1) {
        Some(generation) if generation <= ENGINE_GENERATION_MAX => generation,
        _ => return layout_status::ERR_BAD_VALUE,
    };
    let shared = Arc::new(EngineShared {
        busy: AtomicBool::new(false),
        state: Mutex::new(EngineState::new()),
    });
    registry[slot_index] = EngineSlot {
        slot_generation,
        occupied: true,
        engine: Some(shared),
    };
    unsafe { *out_engine = make_engine_handle(slot_index, slot_generation) };
    layout_status::OK
}

fn engine_reset_body(engine: u64) -> i32 {
    let shared = match resolve_engine(engine) {
        Ok(shared) => shared,
        Err(code) => return code,
    };
    if shared.busy.load(Ordering::SeqCst) {
        return layout_status::ERR_ENGINE_BUSY;
    }
    let mut state = lock_state(&shared);
    let next_generation = match state.node_generation.checked_add(1) {
        Some(generation) if generation <= NODE_GENERATION_MAX => generation,
        _ => return layout_status::ERR_BAD_VALUE,
    };
    state.node_generation = next_generation;
    state.taffy.clear();
    state.nodes.clear();
    state.reverse.clear();
    state.next_node_seq = 0;
    state.failed = false;
    layout_status::OK
}

fn engine_dispose_body(engine: u64) -> i32 {
    let (slot_index, slot_generation) = engine_handle_parts(engine);
    let shared = {
        let mut registry = registry_lock();
        if slot_index >= registry.len() {
            return layout_status::ERR_BAD_HANDLE;
        }
        let slot = &mut registry[slot_index];
        if !slot.occupied || slot.slot_generation != slot_generation {
            return layout_status::ERR_STALE_HANDLE;
        }
        let shared = match slot.engine.clone() {
            Some(shared) => shared,
            None => return layout_status::ERR_STALE_HANDLE,
        };
        if shared.busy.load(Ordering::SeqCst) {
            // Reset/disposal happens only after active native calls return.
            return layout_status::ERR_ENGINE_BUSY;
        }
        slot.occupied = false;
        slot.engine = None;
        shared
    };
    // Drop our reference after the registry lock is released. In-flight
    // callers hold their own clones; the engine state is destroyed once the
    // last in-flight call finishes (deferred native destruction).
    drop(shared);
    layout_status::OK
}

fn node_create_body(
    engine: u64,
    style: *const GpuiGoLayoutStyleRecord,
    children: *const u64,
    child_count: u32,
    out_node: *mut u64,
) -> i32 {
    if style.is_null() || out_node.is_null() {
        return layout_status::ERR_NULL_ARG;
    }
    if child_count > 0 && children.is_null() {
        return layout_status::ERR_NULL_ARG;
    }
    let record: &GpuiGoLayoutStyleRecord = unsafe { &*style };
    let taffy_style = match into_taffy_style(record) {
        Ok(style) => style,
        Err(code) => return code,
    };
    let shared = match resolve_engine(engine) {
        Ok(shared) => shared,
        Err(code) => return code,
    };
    if shared.busy.load(Ordering::SeqCst) {
        return layout_status::ERR_ENGINE_BUSY;
    }
    let mut state = lock_state(&shared);
    if state.failed {
        return layout_status::ERR_ENGINE_FAILED;
    }

    // Resolve and validate children before touching the tree.
    let mut child_nodes: Vec<NodeId> = Vec::with_capacity(child_count as usize);
    for index in 0..child_count {
        let handle = unsafe { *children.add(index as usize) };
        let node = match state.resolve_node(handle) {
            Ok(node) => node,
            Err(code) => return code,
        };
        if state.taffy.parent(node).is_some() {
            return layout_status::ERR_NODE_ATTACHED;
        }
        child_nodes.push(node);
    }

    // Duplicate children would alias one node under two parents.
    let mut unique = child_nodes.clone();
    unique.sort_by_key(|&node| u64::from(node));
    if unique.windows(2).any(|pair| pair[0] == pair[1]) {
        return layout_status::ERR_BAD_HANDLE;
    }

    let node_id = if child_nodes.is_empty() {
        state.taffy.new_leaf(taffy_style)
    } else {
        state.taffy.new_with_children(taffy_style, &child_nodes)
    }
    .expect("gpui-go: taffy node creation failed");
    let handle = match state.alloc_handle(node_id) {
        Ok(handle) => handle,
        Err(code) => return code,
    };
    unsafe { *out_node = handle };
    layout_status::OK
}

fn node_set_style_body(
    engine: u64,
    node: u64,
    style: *const GpuiGoLayoutStyleRecord,
) -> i32 {
    if style.is_null() {
        return layout_status::ERR_NULL_ARG;
    }
    let record: &GpuiGoLayoutStyleRecord = unsafe { &*style };
    let taffy_style = match into_taffy_style(record) {
        Ok(style) => style,
        Err(code) => return code,
    };
    let shared = match resolve_engine(engine) {
        Ok(shared) => shared,
        Err(code) => return code,
    };
    if shared.busy.load(Ordering::SeqCst) {
        return layout_status::ERR_ENGINE_BUSY;
    }
    let mut state = lock_state(&shared);
    if state.failed {
        return layout_status::ERR_ENGINE_FAILED;
    }
    let node_id = match state.resolve_node(node) {
        Ok(node_id) => node_id,
        Err(code) => return code,
    };
    state
        .taffy
        .set_style(node_id, taffy_style)
        .expect("gpui-go: taffy set_style failed");
    layout_status::OK
}

fn node_set_measure_body(engine: u64, node: u64, callback_token: u64) -> i32 {
    let shared = match resolve_engine(engine) {
        Ok(shared) => shared,
        Err(code) => return code,
    };
    if shared.busy.load(Ordering::SeqCst) {
        return layout_status::ERR_ENGINE_BUSY;
    }
    let mut state = lock_state(&shared);
    if state.failed {
        return layout_status::ERR_ENGINE_FAILED;
    }
    let node_id = match state.resolve_node(node) {
        Ok(node_id) => node_id,
        Err(code) => return code,
    };
    let context = MeasureContext { node_handle: node, callback_token };
    state
        .taffy
        .set_node_context(node_id, Some(context))
        .expect("gpui-go: taffy set_node_context failed");
    layout_status::OK
}

fn node_remove_subtree_body(engine: u64, node: u64) -> i32 {
    let shared = match resolve_engine(engine) {
        Ok(shared) => shared,
        Err(code) => return code,
    };
    if shared.busy.load(Ordering::SeqCst) {
        return layout_status::ERR_ENGINE_BUSY;
    }
    let mut state = lock_state(&shared);
    if state.failed {
        return layout_status::ERR_ENGINE_FAILED;
    }
    let root = match state.resolve_node(node) {
        Ok(node_id) => node_id,
        Err(code) => return code,
    };
    let parent = state.taffy.parent(root);

    // Iterative preorder collection (no recursion: caller-controlled depth
    // must not overflow the stack).
    let mut order: Vec<NodeId> = Vec::new();
    let mut stack: Vec<NodeId> = vec![root];
    while let Some(current) = stack.pop() {
        order.push(current);
        let children = state
            .taffy
            .children(current)
            .expect("gpui-go: taffy children failed");
        for child in children.iter().rev() {
            stack.push(*child);
        }
    }
    // Remove children before parents (reverse preorder) so no node is left
    // orphaned in the tree.
    for node_id in order.iter().rev() {
        state
            .taffy
            .remove(*node_id)
            .expect("gpui-go: taffy remove failed");
        if let Some(&sequence) = state.reverse.get(node_id) {
            state.reverse.remove(node_id);
            state.nodes.remove(&sequence);
        }
    }
    if let Some(parent) = parent {
        if state.reverse.contains_key(&parent) {
            state
                .taffy
                .mark_dirty(parent)
                .expect("gpui-go: taffy mark_dirty failed");
        }
    }
    layout_status::OK
}

fn compute_body(
    engine: u64,
    root: u64,
    available: *const GpuiAvailSize,
    compute_token: u64,
    out_flags: *mut u32,
) -> i32 {
    if available.is_null() || out_flags.is_null() {
        return layout_status::ERR_NULL_ARG;
    }
    let available_rec: &GpuiAvailSize = unsafe { &*available };
    let available_space = TaffySize {
        width: match avail_from(available_rec.width) {
            Ok(value) => value,
            Err(code) => return code,
        },
        height: match avail_from(available_rec.height) {
            Ok(value) => value,
            Err(code) => return code,
        },
    };

    let shared = match resolve_engine(engine) {
        Ok(shared) => shared,
        Err(code) => return code,
    };
    // Same-engine re-entry (e.g. a measure trampoline computing the engine
    // it is measuring) is rejected here, before the state lock.
    if shared.busy.swap(true, Ordering::SeqCst) {
        return layout_status::ERR_ENGINE_BUSY;
    }
    let _busy_guard = BusyGuard(shared.clone());
    let mut state = lock_state(&shared);
    if state.failed {
        return layout_status::ERR_ENGINE_FAILED;
    }
    let root_node = match state.resolve_node(root) {
        Ok(node_id) => node_id,
        Err(code) => return code,
    };

    let mut callbacks_invoked = false;
    let mut failure: Option<ComputeFailure> = None;
    let trampoline = TRAMPOLINE.load(Ordering::Acquire);

    state
        .taffy
        .compute_layout_with_measure(
            root_node,
            available_space,
            |known_dimensions,
             available_space,
             _node_id,
             node_context,
             _style| {
                let Some(context) = node_context else {
                    // Unmeasured leaves: the pinned gpui closure returns the
                    // default size for nodes without a measure context.
                    return TaffySize::default();
                };
                // Suppress further user callbacks once a failure is latched.
                if failure.is_some() {
                    return TaffySize::zero();
                }
                if trampoline == 0 {
                    failure = Some(ComputeFailure::MissingTrampoline);
                    return TaffySize::zero();
                }
                let trampoline_fn: GpuiGoLayoutTrampoline =
                    unsafe { std::mem::transmute(trampoline) };
                let (avail_width_tag, avail_width_bits) =
                    avail_parts(&available_space.width);
                let (avail_height_tag, avail_height_bits) =
                    avail_parts(&available_space.height);
                let mut request = GpuiMeasureRequest {
                    engine,
                    node: context.node_handle,
                    callback_token: context.callback_token,
                    compute_token,
                    known_width_present: known_dimensions.width.is_some() as u32,
                    known_height_present: known_dimensions.height.is_some() as u32,
                    avail_width_tag,
                    avail_height_tag,
                    known_width_bits: known_dimensions.width.map(f32::to_bits).unwrap_or(0),
                    known_height_bits: known_dimensions.height.map(f32::to_bits).unwrap_or(0),
                    avail_width_bits,
                    avail_height_bits,
                    reserved: [0; 2],
                };
                let mut response =
                    GpuiMeasureResponse { status: -1, width_bits: 0, height_bits: 0 };
                let call_code = unsafe { trampoline_fn(&mut request, &mut response) };
                callbacks_invoked = true;
                if call_code != 0 || response.status != 0 {
                    failure = Some(ComputeFailure::CallbackFailed);
                    return TaffySize::zero();
                }
                let width = f32::from_bits(response.width_bits);
                let height = f32::from_bits(response.height_bits);
                if !width.is_finite() || !height.is_finite() {
                    // A non-finite measured size would poison the layout;
                    // treat it as a callback failure.
                    failure = Some(ComputeFailure::CallbackFailed);
                    return TaffySize::zero();
                }
                TaffySize { width, height }
            },
        )
        .expect("gpui-go: taffy compute_layout failed");

    unsafe { *out_flags = u32::from(callbacks_invoked) };
    match failure {
        Some(ComputeFailure::CallbackFailed) => {
            state.failed = true;
            layout_status::ERR_CALLBACK
        }
        Some(ComputeFailure::MissingTrampoline) => layout_status::ERR_NO_TRAMPOLINE,
        None => layout_status::OK,
    }
}

fn layout_record_from(node: NodeId, state: &EngineState) -> GpuiGoLayoutRecord {
    let layout = state
        .taffy
        .layout(node)
        .expect("gpui-go: taffy layout lookup failed");
    GpuiGoLayoutRecord {
        order: layout.order as i32,
        location_x: layout.location.x,
        location_y: layout.location.y,
        size_w: layout.size.width,
        size_h: layout.size.height,
        content_w: layout.content_size.width,
        content_h: layout.content_size.height,
        scrollbar_w: layout.scrollbar_size.width,
        scrollbar_h: layout.scrollbar_size.height,
        border_top: layout.border.top,
        border_right: layout.border.right,
        border_bottom: layout.border.bottom,
        border_left: layout.border.left,
        padding_top: layout.padding.top,
        padding_right: layout.padding.right,
        padding_bottom: layout.padding.bottom,
        padding_left: layout.padding.left,
        record_size: std::mem::size_of::<GpuiGoLayoutRecord>() as u32,
    }
}

fn node_layout_body(engine: u64, node: u64, out: *mut GpuiGoLayoutRecord) -> i32 {
    if out.is_null() {
        return layout_status::ERR_NULL_ARG;
    }
    let shared = match resolve_engine(engine) {
        Ok(shared) => shared,
        Err(code) => return code,
    };
    if shared.busy.load(Ordering::SeqCst) {
        return layout_status::ERR_ENGINE_BUSY;
    }
    let state = lock_state(&shared);
    if state.failed {
        return layout_status::ERR_ENGINE_FAILED;
    }
    let node_id = match state.resolve_node(node) {
        Ok(node_id) => node_id,
        Err(code) => return code,
    };
    let record = layout_record_from(node_id, &state);
    unsafe { *out = record };
    layout_status::OK
}

fn node_parent_body(engine: u64, node: u64, out_parent: *mut u64) -> i32 {
    if out_parent.is_null() {
        return layout_status::ERR_NULL_ARG;
    }
    let shared = match resolve_engine(engine) {
        Ok(shared) => shared,
        Err(code) => return code,
    };
    if shared.busy.load(Ordering::SeqCst) {
        return layout_status::ERR_ENGINE_BUSY;
    }
    let state = lock_state(&shared);
    if state.failed {
        return layout_status::ERR_ENGINE_FAILED;
    }
    let node_id = match state.resolve_node(node) {
        Ok(node_id) => node_id,
        Err(code) => return code,
    };
    let parent = match state.taffy.parent(node_id) {
        None => {
            unsafe { *out_parent = 0 };
            return layout_status::OK;
        }
        Some(parent) => parent,
    };
    let handle = match state.handle_of(parent) {
        Ok(handle) => handle,
        Err(code) => return code,
    };
    unsafe { *out_parent = handle };
    layout_status::OK
}

fn node_child_count_body(engine: u64, node: u64, out_count: *mut u32) -> i32 {
    if out_count.is_null() {
        return layout_status::ERR_NULL_ARG;
    }
    let shared = match resolve_engine(engine) {
        Ok(shared) => shared,
        Err(code) => return code,
    };
    if shared.busy.load(Ordering::SeqCst) {
        return layout_status::ERR_ENGINE_BUSY;
    }
    let state = lock_state(&shared);
    if state.failed {
        return layout_status::ERR_ENGINE_FAILED;
    }
    let node_id = match state.resolve_node(node) {
        Ok(node_id) => node_id,
        Err(code) => return code,
    };
    let count = state
        .taffy
        .children(node_id)
        .expect("gpui-go: taffy children failed")
        .len() as u32;
    unsafe { *out_count = count };
    layout_status::OK
}

fn node_child_body(engine: u64, node: u64, index: u32, out_child: *mut u64) -> i32 {
    if out_child.is_null() {
        return layout_status::ERR_NULL_ARG;
    }
    let shared = match resolve_engine(engine) {
        Ok(shared) => shared,
        Err(code) => return code,
    };
    if shared.busy.load(Ordering::SeqCst) {
        return layout_status::ERR_ENGINE_BUSY;
    }
    let state = lock_state(&shared);
    if state.failed {
        return layout_status::ERR_ENGINE_FAILED;
    }
    let node_id = match state.resolve_node(node) {
        Ok(node_id) => node_id,
        Err(code) => return code,
    };
    let children = state
        .taffy
        .children(node_id)
        .expect("gpui-go: taffy children failed");
    let child = match children.get(index as usize) {
        Some(&child) => child,
        None => return layout_status::ERR_BAD_VALUE,
    };
    let handle = match state.handle_of(child) {
        Ok(handle) => handle,
        Err(code) => return code,
    };
    unsafe { *out_child = handle };
    layout_status::OK
}

fn dump_body(
    engine: u64,
    root: u64,
    node_ids: *mut u64,
    records: *mut GpuiGoLayoutRecord,
    capacity: u32,
    out_count: *mut u32,
) -> i32 {
    if out_count.is_null() {
        return layout_status::ERR_NULL_ARG;
    }
    if capacity > 0 && (node_ids.is_null() || records.is_null()) {
        return layout_status::ERR_NULL_ARG;
    }
    let shared = match resolve_engine(engine) {
        Ok(shared) => shared,
        Err(code) => return code,
    };
    if shared.busy.load(Ordering::SeqCst) {
        return layout_status::ERR_ENGINE_BUSY;
    }
    let state = lock_state(&shared);
    if state.failed {
        return layout_status::ERR_ENGINE_FAILED;
    }
    let root_node = match state.resolve_node(root) {
        Ok(node_id) => node_id,
        Err(code) => return code,
    };

    // Iterative preorder walk (root first, then children in order).
    let mut walked: Vec<(NodeId, GpuiGoLayoutRecord)> = Vec::new();
    let mut stack: Vec<NodeId> = vec![root_node];
    while let Some(current) = stack.pop() {
        let record = layout_record_from(current, &state);
        walked.push((current, record));
        let children = state
            .taffy
            .children(current)
            .expect("gpui-go: taffy children failed");
        for child in children.iter().rev() {
            stack.push(*child);
        }
    }

    let needed = walked.len() as u32;
    unsafe { *out_count = needed };
    if needed > capacity {
        return layout_status::ERR_CAPACITY;
    }
    for (index, (node_id, record)) in walked.into_iter().enumerate() {
        let handle = match state.handle_of(node_id) {
            Ok(handle) => handle,
            Err(code) => return code,
        };
        unsafe {
            *node_ids.add(index) = handle;
            *records.add(index) = record;
        }
    }
    layout_status::OK
}

fn set_trampoline_body(trampoline: GpuiGoLayoutTrampoline) -> i32 {
    if trampoline as usize == 0 {
        return layout_status::ERR_NULL_ARG;
    }
    TRAMPOLINE.store(trampoline as usize, Ordering::Release);
    layout_status::OK
}

// ---------------------------------------------------------------------------
// Table entries (panic boundary wrappers)
// ---------------------------------------------------------------------------

unsafe extern "system" fn layout_engine_create(out_engine: *mut u64) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| engine_create_body(out_engine)));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            layout_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn layout_engine_reset(engine: u64) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| engine_reset_body(engine)));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_engine_failed(engine);
            layout_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn layout_engine_dispose(engine: u64) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| engine_dispose_body(engine)));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_engine_failed(engine);
            layout_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn layout_node_create(
    engine: u64,
    style: *const GpuiGoLayoutStyleRecord,
    children: *const u64,
    child_count: u32,
    out_node: *mut u64,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| {
        node_create_body(engine, style, children, child_count, out_node)
    }));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_engine_failed(engine);
            layout_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn layout_node_set_style(
    engine: u64,
    node: u64,
    style: *const GpuiGoLayoutStyleRecord,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| node_set_style_body(engine, node, style)));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_engine_failed(engine);
            layout_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn layout_node_set_measure(
    engine: u64,
    node: u64,
    callback_token: u64,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| node_set_measure_body(engine, node, callback_token)));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_engine_failed(engine);
            layout_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn layout_node_remove_subtree(engine: u64, node: u64) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| node_remove_subtree_body(engine, node)));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_engine_failed(engine);
            layout_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn layout_compute(
    engine: u64,
    root: u64,
    available: *const GpuiAvailSize,
    compute_token: u64,
    out_flags: *mut u32,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| compute_body(engine, root, available, compute_token, out_flags)));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_engine_failed(engine);
            layout_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn layout_node_layout(
    engine: u64,
    node: u64,
    out: *mut GpuiGoLayoutRecord,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| node_layout_body(engine, node, out)));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_engine_failed(engine);
            layout_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn layout_node_parent(
    engine: u64,
    node: u64,
    out_parent: *mut u64,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| node_parent_body(engine, node, out_parent)));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_engine_failed(engine);
            layout_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn layout_node_child_count(
    engine: u64,
    node: u64,
    out_count: *mut u32,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| node_child_count_body(engine, node, out_count)));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_engine_failed(engine);
            layout_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn layout_node_child(
    engine: u64,
    node: u64,
    index: u32,
    out_child: *mut u64,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| node_child_body(engine, node, index, out_child)));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_engine_failed(engine);
            layout_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn layout_dump(
    engine: u64,
    root: u64,
    node_ids: *mut u64,
    records: *mut GpuiGoLayoutRecord,
    capacity: u32,
    out_count: *mut u32,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| dump_body(engine, root, node_ids, records, capacity, out_count)));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            mark_engine_failed(engine);
            layout_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn layout_set_trampoline(trampoline: GpuiGoLayoutTrampoline) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| set_trampoline_body(trampoline)));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            layout_status::ERR_PANIC
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::Mutex as TestMutex;

    /// The trampoline is process-global; tests that touch it are serialized
    /// and restore the previous value.
    static TRAMPOLINE_TEST_LOCK: TestMutex<()> = TestMutex::new(());

    fn absent_len() -> GpuiLen {
        GpuiLen { tag: len_tag::ABSENT, bits: 0 }
    }

    fn auto_len() -> GpuiLen {
        GpuiLen { tag: len_tag::AUTO, bits: 0 }
    }

    fn px(value: f32) -> GpuiLen {
        GpuiLen { tag: len_tag::DEFINITE, bits: value.to_bits() }
    }

    fn pct(value: f32) -> GpuiLen {
        GpuiLen { tag: len_tag::PERCENT, bits: value.to_bits() }
    }

    /// A record where every optional field is absent and every defaulted
    /// field carries the gpui default. Must translate to exactly the Taffy
    /// default style.
    fn minimal_record() -> GpuiGoLayoutStyleRecord {
        GpuiGoLayoutStyleRecord {
            display: 0,
            position: 0,
            overflow_x: 0,
            overflow_y: 0,
            scrollbar_width: absent_len(),
            inset: GpuiEdges {
                top: absent_len(),
                right: absent_len(),
                bottom: absent_len(),
                left: absent_len(),
            },
            size: GpuiSizeL { width: absent_len(), height: absent_len() },
            min_size: GpuiSizeL { width: absent_len(), height: absent_len() },
            max_size: GpuiSizeL { width: absent_len(), height: absent_len() },
            aspect_ratio_present: 0,
            aspect_ratio_bits: 0,
            margin: GpuiEdges {
                top: absent_len(),
                right: absent_len(),
                bottom: absent_len(),
                left: absent_len(),
            },
            padding: GpuiEdges {
                top: absent_len(),
                right: absent_len(),
                bottom: absent_len(),
                left: absent_len(),
            },
            border_top: 0,
            border_right: 0,
            border_bottom: 0,
            border_left: 0,
            align_items_present: 0,
            align_items: 0,
            align_self_present: 0,
            align_self: 0,
            align_content_present: 0,
            align_content: 0,
            justify_content_present: 0,
            justify_content: 0,
            gap: GpuiSizeL { width: absent_len(), height: absent_len() },
            flex_direction: 0,
            flex_wrap: 0,
            flex_basis: absent_len(),
            flex_grow_present: 0,
            flex_grow_bits: 0,
            flex_shrink_present: 0,
            flex_shrink_bits: 0,
            grid_template_rows_present: 0,
            grid_template_rows_repeat: 0,
            grid_template_rows_min_size: 0,
            grid_template_columns_present: 0,
            grid_template_columns_repeat: 0,
            grid_template_columns_min_size: 0,
            grid_placement_present: 0,
            grid_row_start_kind: 0,
            grid_row_start_value: 0,
            grid_row_end_kind: 0,
            grid_row_end_value: 0,
            grid_column_start_kind: 0,
            grid_column_start_value: 0,
            grid_column_end_kind: 0,
            grid_column_end_value: 0,
            record_size: std::mem::size_of::<GpuiGoLayoutStyleRecord>() as u32,
        }
    }

    fn create_engine() -> u64 {
        let mut handle = 0;
        let code = engine_create_body(&mut handle);
        assert_eq!(code, layout_status::OK, "engine create failed");
        handle
    }

    fn create_node(engine: u64, record: &GpuiGoLayoutStyleRecord) -> u64 {
        create_node_with_children(engine, record, &[])
    }

    fn create_node_with_children(
        engine: u64,
        record: &GpuiGoLayoutStyleRecord,
        children: &[u64],
    ) -> u64 {
        let mut handle = 0;
        let code = node_create_body(
            engine,
            record,
            children.as_ptr(),
            children.len() as u32,
            &mut handle,
        );
        assert_eq!(code, layout_status::OK, "node create failed");
        handle
    }

    fn definite_avail(width: f32, height: f32) -> GpuiAvailSize {
        GpuiAvailSize {
            width: GpuiAvail { tag: 0, bits: width.to_bits() },
            height: GpuiAvail { tag: 0, bits: height.to_bits() },
        }
    }

    fn compute(engine: u64, root: u64, available: GpuiAvailSize) -> (i32, u32) {
        let mut flags = 0xFFFF_FFFF;
        let code = compute_body(engine, root, &available, 0, &mut flags);
        (code, flags)
    }

    fn node_layout(engine: u64, node: u64) -> GpuiGoLayoutRecord {
        let mut record = GpuiGoLayoutRecord {
            order: -1,
            location_x: f32::NAN,
            location_y: f32::NAN,
            size_w: f32::NAN,
            size_h: f32::NAN,
            content_w: f32::NAN,
            content_h: f32::NAN,
            scrollbar_w: f32::NAN,
            scrollbar_h: f32::NAN,
            border_top: f32::NAN,
            border_right: f32::NAN,
            border_bottom: f32::NAN,
            border_left: f32::NAN,
            padding_top: f32::NAN,
            padding_right: f32::NAN,
            padding_bottom: f32::NAN,
            padding_left: f32::NAN,
            record_size: 0,
        };
        let code = node_layout_body(engine, node, &mut record);
        assert_eq!(code, layout_status::OK, "node layout failed");
        record
    }

    fn blank_layout_record() -> GpuiGoLayoutRecord {
        GpuiGoLayoutRecord {
            order: -1,
            location_x: f32::NAN,
            location_y: f32::NAN,
            size_w: f32::NAN,
            size_h: f32::NAN,
            content_w: f32::NAN,
            content_h: f32::NAN,
            scrollbar_w: f32::NAN,
            scrollbar_h: f32::NAN,
            border_top: f32::NAN,
            border_right: f32::NAN,
            border_bottom: f32::NAN,
            border_left: f32::NAN,
            padding_top: f32::NAN,
            padding_right: f32::NAN,
            padding_bottom: f32::NAN,
            padding_left: f32::NAN,
            record_size: 0,
        }
    }

    #[test]
    fn minimal_record_translates_to_taffy_default() {
        let style = into_taffy_style(&minimal_record()).expect("translation failed");
        assert_eq!(style, TaffyStyle::default());
    }

    #[test]
    fn style_translation_maps_forwarded_fields() {
        let mut record = minimal_record();
        record.display = 1; // block
        record.position = 1; // absolute
        record.overflow_x = 3; // scroll
        record.overflow_y = 1; // clip
        record.scrollbar_width = px(7.0);
        record.inset.top = auto_len();
        record.inset.right = px(4.0);
        record.inset.bottom = pct(0.25);
        record.inset.left = px(-2.0);
        record.size.width = px(300.0);
        record.size.height = px(200.0);
        record.min_size.width = pct(0.5);
        record.max_size.height = px(80.0);
        record.aspect_ratio_present = 1;
        record.aspect_ratio_bits = 2.0_f32.to_bits();
        record.margin.top = auto_len();
        record.margin.left = px(6.0);
        record.padding.top = px(6.5);
        record.padding.left = pct(0.1);
        record.border_top = 2.0_f32.to_bits();
        record.border_left = 0.5_f32.to_bits();
        record.align_items_present = 1;
        record.align_items = 4; // center
        record.align_self_present = 1;
        record.align_self = 6; // stretch
        record.align_content_present = 1;
        record.align_content = 6; // space-between
        record.justify_content_present = 1;
        record.justify_content = 7; // space-evenly
        record.gap.width = px(10.0);
        record.gap.height = pct(0.05);
        record.flex_direction = 1; // column
        record.flex_wrap = 1; // wrap
        record.flex_basis = pct(0.5);
        record.flex_grow_present = 1;
        record.flex_grow_bits = 2.0_f32.to_bits();
        record.flex_shrink_present = 1;
        record.flex_shrink_bits = 0.0_f32.to_bits();
        record.grid_template_rows_present = 1;
        record.grid_template_rows_repeat = 3;
        record.grid_template_rows_min_size = 1; // min-content
        record.grid_template_columns_present = 1;
        record.grid_template_columns_repeat = 2;
        record.grid_template_columns_min_size = 2; // max-content
        record.grid_placement_present = 1;
        record.grid_row_start_kind = 1;
        record.grid_row_start_value = 2;
        record.grid_row_end_kind = 2;
        record.grid_row_end_value = 4;
        record.grid_column_start_kind = 1;
        record.grid_column_start_value = (-1_i16) as u16 as u32;
        record.grid_column_end_kind = 0;

        let style = into_taffy_style(&record).expect("translation failed");
        assert_eq!(style.display, Display::Block);
        assert_eq!(style.position, Position::Absolute);
        assert_eq!(style.overflow.x, Overflow::Scroll);
        assert_eq!(style.overflow.y, Overflow::Clip);
        assert_eq!(style.scrollbar_width, 7.0);
        assert_eq!(style.inset.top, LengthPercentageAuto::auto());
        assert_eq!(style.inset.right, LengthPercentageAuto::length(4.0));
        assert_eq!(style.inset.bottom, LengthPercentageAuto::percent(0.25));
        assert_eq!(style.inset.left, LengthPercentageAuto::length(-2.0));
        assert_eq!(style.size.width, Dimension::length(300.0));
        assert_eq!(style.size.height, Dimension::length(200.0));
        assert_eq!(style.min_size.width, Dimension::percent(0.5));
        assert_eq!(style.max_size.height, Dimension::length(80.0));
        assert_eq!(style.aspect_ratio, Some(2.0));
        assert_eq!(style.margin.top, LengthPercentageAuto::auto());
        assert_eq!(style.margin.left, LengthPercentageAuto::length(6.0));
        assert_eq!(style.padding.top, LengthPercentage::length(6.5));
        assert_eq!(style.padding.left, LengthPercentage::percent(0.1));
        assert_eq!(style.border.top, LengthPercentage::length(2.0));
        assert_eq!(style.border.left, LengthPercentage::length(0.5));
        assert_eq!(style.align_items, Some(AlignItems::CENTER));
        assert_eq!(style.align_self, Some(AlignItems::STRETCH));
        assert_eq!(style.align_content, Some(AlignContent::SPACE_BETWEEN));
        assert_eq!(style.justify_content, Some(AlignContent::SPACE_EVENLY));
        assert_eq!(style.gap.width, LengthPercentage::length(10.0));
        assert_eq!(style.gap.height, LengthPercentage::percent(0.05));
        assert_eq!(style.flex_direction, FlexDirection::Column);
        assert_eq!(style.flex_wrap, FlexWrap::Wrap);
        assert_eq!(style.flex_basis, Dimension::percent(0.5));
        assert_eq!(style.flex_grow, 2.0);
        assert_eq!(style.flex_shrink, 0.0);
        // Grid: repeat(N, minmax(<min>, 1fr)) as the pinned to_grid_repeat.
        assert_eq!(
            style.grid_template_rows,
            vec![repeat(3_u16, vec![minmax(min_content(), fr(1.0_f32))])]
        );
        assert_eq!(
            style.grid_template_columns,
            vec![repeat(2_u16, vec![minmax(max_content(), fr(1.0_f32))])]
        );
        assert_eq!(
            style.grid_row,
            Line {
                start: taffy::style::GridPlacement::from_line_index(2),
                end: taffy::style::GridPlacement::from_span(4)
            }
        );
        assert_eq!(
            style.grid_column,
            Line {
                start: taffy::style::GridPlacement::from_line_index(-1),
                end: taffy::style::GridPlacement::Auto
            }
        );
    }

    #[test]
    fn style_translation_rejects_invalid_values() {
        let bad_tags = |mut record: GpuiGoLayoutStyleRecord| {
            record.record_size = std::mem::size_of::<GpuiGoLayoutStyleRecord>() as u32;
            record
        };

        let mut record = bad_tags(minimal_record());
        record.record_size = 335;
        assert_eq!(into_taffy_style(&record), Err(layout_status::ERR_BAD_VALUE));

        let mut record = bad_tags(minimal_record());
        record.display = 4;
        assert_eq!(into_taffy_style(&record), Err(layout_status::ERR_BAD_VALUE));

        let mut record = bad_tags(minimal_record());
        record.overflow_x = 4;
        assert_eq!(into_taffy_style(&record), Err(layout_status::ERR_BAD_VALUE));

        let mut record = bad_tags(minimal_record());
        record.scrollbar_width = pct(0.5);
        assert_eq!(into_taffy_style(&record), Err(layout_status::ERR_BAD_VALUE));

        // Padding has no auto.
        let mut record = bad_tags(minimal_record());
        record.padding.top = auto_len();
        assert_eq!(into_taffy_style(&record), Err(layout_status::ERR_BAD_VALUE));

        // Reserved min/max-content tags are rejected.
        let mut record = bad_tags(minimal_record());
        record.size.width = GpuiLen { tag: 4, bits: 0 };
        assert_eq!(into_taffy_style(&record), Err(layout_status::ERR_BAD_VALUE));

        let mut record = bad_tags(minimal_record());
        record.flex_direction = 4;
        assert_eq!(into_taffy_style(&record), Err(layout_status::ERR_BAD_VALUE));

        let mut record = bad_tags(minimal_record());
        record.flex_wrap = 3;
        assert_eq!(into_taffy_style(&record), Err(layout_status::ERR_BAD_VALUE));

        let mut record = bad_tags(minimal_record());
        record.align_items_present = 2;
        assert_eq!(into_taffy_style(&record), Err(layout_status::ERR_BAD_VALUE));

        let mut record = bad_tags(minimal_record());
        record.align_items_present = 1;
        record.align_items = 7;
        assert_eq!(into_taffy_style(&record), Err(layout_status::ERR_BAD_VALUE));

        let mut record = bad_tags(minimal_record());
        record.justify_content_present = 1;
        record.justify_content = 9;
        assert_eq!(into_taffy_style(&record), Err(layout_status::ERR_BAD_VALUE));

        let mut record = bad_tags(minimal_record());
        record.grid_template_rows_present = 1;
        record.grid_template_rows_repeat = 65_536;
        assert_eq!(into_taffy_style(&record), Err(layout_status::ERR_BAD_VALUE));

        let mut record = bad_tags(minimal_record());
        record.grid_template_columns_min_size = 3;
        record.grid_template_columns_present = 1;
        assert_eq!(into_taffy_style(&record), Err(layout_status::ERR_BAD_VALUE));

        let mut record = bad_tags(minimal_record());
        record.grid_placement_present = 1;
        record.grid_row_start_kind = 1;
        record.grid_row_start_value = 65_536;
        assert_eq!(into_taffy_style(&record), Err(layout_status::ERR_BAD_VALUE));

        let mut record = bad_tags(minimal_record());
        record.grid_placement_present = 1;
        record.grid_row_start_kind = 3;
        assert_eq!(into_taffy_style(&record), Err(layout_status::ERR_BAD_VALUE));
    }

    #[test]
    fn table_self_checks_match_memory_truth() {
        assert_eq!(
            LAYOUT_TABLE.size_of_table as usize,
            std::mem::size_of::<GpuiGoLayoutTable>()
        );
        assert_eq!(LAYOUT_TABLE.service_version, GPUI_GO_LAYOUT_SERVICE_VERSION);
        assert_eq!(
            LAYOUT_TABLE.size_of_style_record as usize,
            std::mem::size_of::<GpuiGoLayoutStyleRecord>()
        );
        assert_eq!(
            LAYOUT_TABLE.size_of_layout_record as usize,
            std::mem::size_of::<GpuiGoLayoutRecord>()
        );
        assert_eq!(
            LAYOUT_TABLE.size_of_measure_request as usize,
            std::mem::size_of::<GpuiMeasureRequest>()
        );
        assert_eq!(
            LAYOUT_TABLE.size_of_measure_response as usize,
            std::mem::size_of::<GpuiMeasureResponse>()
        );
        // Every function slot is populated.
        let slots: [usize; 14] = [
            LAYOUT_TABLE.engine_create.map(|f| f as usize).unwrap_or(0),
            LAYOUT_TABLE.engine_reset.map(|f| f as usize).unwrap_or(0),
            LAYOUT_TABLE.engine_dispose.map(|f| f as usize).unwrap_or(0),
            LAYOUT_TABLE.node_create.map(|f| f as usize).unwrap_or(0),
            LAYOUT_TABLE.node_set_style.map(|f| f as usize).unwrap_or(0),
            LAYOUT_TABLE.node_set_measure.map(|f| f as usize).unwrap_or(0),
            LAYOUT_TABLE.node_remove_subtree.map(|f| f as usize).unwrap_or(0),
            LAYOUT_TABLE.compute.map(|f| f as usize).unwrap_or(0),
            LAYOUT_TABLE.node_layout.map(|f| f as usize).unwrap_or(0),
            LAYOUT_TABLE.node_parent.map(|f| f as usize).unwrap_or(0),
            LAYOUT_TABLE.node_child_count.map(|f| f as usize).unwrap_or(0),
            LAYOUT_TABLE.node_child.map(|f| f as usize).unwrap_or(0),
            LAYOUT_TABLE.dump.map(|f| f as usize).unwrap_or(0),
            LAYOUT_TABLE.set_trampoline.map(|f| f as usize).unwrap_or(0),
        ];
        for (index, slot) in slots.iter().enumerate() {
            assert!(*slot != 0, "layout table slot {index} is null");
        }
    }

    #[test]
    fn engine_lifecycle_and_generations() {
        let engine_a = create_engine();
        let (slot_a, gen_a) = engine_handle_parts(engine_a);
        assert_eq!(gen_a, 1, "fresh engines start at generation 1");

        let node = create_node(engine_a, &minimal_record());
        let (node_gen, node_seq) = node_handle_parts(node);
        assert_eq!(node_gen, 1);
        assert_eq!(node_seq, 0);

        // Reset bumps the node generation: old node handles go stale, the
        // engine handle stays valid.
        assert_eq!(engine_reset_body(engine_a), layout_status::OK);
        let mut record = blank_layout_record();
        assert_eq!(
            node_layout_body(engine_a, node, &mut record),
            layout_status::ERR_STALE_HANDLE
        );
        let fresh = create_node(engine_a, &minimal_record());
        let (fresh_gen, fresh_seq) = node_handle_parts(fresh);
        assert_eq!(fresh_gen, 2, "reset bumped the node generation");
        assert_eq!(fresh_seq, 0, "node sequences restart after reset");

        // A second engine at a different slot does not disturb the first.
        let engine_b = create_engine();
        let (slot_b, _) = engine_handle_parts(engine_b);
        assert_ne!(slot_a, slot_b);
        let node_b = create_node(engine_b, &minimal_record());
        let mut record = blank_layout_record();
        assert_eq!(
            node_layout_body(engine_a, node_b, &mut record),
            layout_status::ERR_STALE_HANDLE,
            "node handles from another engine are stale"
        );

        // Disposing engine B makes its handle stale; a new engine at the
        // same slot gets a new slot generation.
        assert_eq!(engine_dispose_body(engine_b), layout_status::OK);
        assert_eq!(
            engine_reset_body(engine_b),
            layout_status::ERR_STALE_HANDLE
        );
        let engine_c = create_engine();
        let (slot_c, gen_c) = engine_handle_parts(engine_c);
        assert_eq!(slot_c, slot_b, "the freed slot is reused");
        assert_eq!(gen_c, 2, "slot reuse advances the slot generation");
        assert_eq!(
            engine_reset_body(engine_b),
            layout_status::ERR_STALE_HANDLE,
            "the disposed handle remains stale"
        );

        // Garbage handles are distinguished from stale ones: the current
        // node generation with a never-issued sequence is malformed.
        let mut garbage = blank_layout_record();
        assert_eq!(
            node_layout_body(engine_a, make_node_handle(2, 999), &mut garbage),
            layout_status::ERR_BAD_HANDLE
        );
        assert_eq!(
            engine_reset_body(make_engine_handle(9999, 1)),
            layout_status::ERR_BAD_HANDLE
        );
        assert_eq!(
            engine_reset_body(make_engine_handle(slot_a, gen_a + 1)),
            layout_status::ERR_STALE_HANDLE
        );

        // Null argument checks.
        assert_eq!(engine_create_body(std::ptr::null_mut()), layout_status::ERR_NULL_ARG);
    }

    #[test]
    fn flex_tree_computes_known_geometry() {
        let engine = create_engine();

        let mut root_record = minimal_record();
        root_record.size = GpuiSizeL { width: px(300.0), height: px(200.0) };
        let mut child_a = minimal_record();
        child_a.size = GpuiSizeL { width: px(100.0), height: px(50.0) };
        let mut child_b = minimal_record();
        child_b.flex_grow_present = 1;
        child_b.flex_grow_bits = 1.0_f32.to_bits();

        let a = create_node(engine, &child_a);
        let b = create_node(engine, &child_b);
        let root = create_node_with_children(engine, &root_record, &[a, b]);

        // Attaching an already-attached child is a caller error.
        let mut out = 0;
        assert_eq!(
            node_create_body(engine, &minimal_record(), &a, 1, &mut out),
            layout_status::ERR_NODE_ATTACHED
        );
        // Duplicate children alias one node under two parents and are
        // rejected too (the duplicate must be unattached to reach that
        // check).
        let duplicate = create_node(engine, &minimal_record());
        let children = [duplicate, duplicate];
        assert_eq!(
            node_create_body(engine, &minimal_record(), children.as_ptr(), 2, &mut out),
            layout_status::ERR_BAD_HANDLE
        );

        let (code, flags) = compute(engine, root, definite_avail(300.0, 200.0));
        assert_eq!(code, layout_status::OK);
        assert_eq!(flags & 1, 0, "no measured callbacks in a plain tree");

        let root_layout = node_layout(engine, root);
        assert_eq!((root_layout.location_x, root_layout.location_y), (0.0, 0.0));
        assert_eq!((root_layout.size_w, root_layout.size_h), (300.0, 200.0));

        let a_layout = node_layout(engine, a);
        assert_eq!((a_layout.location_x, a_layout.location_y), (0.0, 0.0));
        assert_eq!((a_layout.size_w, a_layout.size_h), (100.0, 50.0));

        let b_layout = node_layout(engine, b);
        assert_eq!((b_layout.location_x, b_layout.location_y), (100.0, 0.0));
        assert_eq!((b_layout.size_w, b_layout.size_h), (200.0, 200.0));

        // Parent/child relations.
        let mut parent = 0xDEAD;
        assert_eq!(node_parent_body(engine, a, &mut parent), layout_status::OK);
        assert_eq!(parent, root);
        assert_eq!(node_parent_body(engine, root, &mut parent), layout_status::OK);
        assert_eq!(parent, 0, "the root has no parent");
        let mut count = 0;
        assert_eq!(node_child_count_body(engine, root, &mut count), layout_status::OK);
        assert_eq!(count, 2);
        let mut child_handle = 0;
        assert_eq!(node_child_body(engine, root, 1, &mut child_handle), layout_status::OK);
        assert_eq!(child_handle, b);
        assert_eq!(
            node_child_body(engine, root, 2, &mut child_handle),
            layout_status::ERR_BAD_VALUE
        );

        // Dump: preorder [root, a, b].
        let mut ids = [0_u64; 3];
        let mut records = [node_layout(engine, root); 3];
        let mut count = 0;
        assert_eq!(
            dump_body(engine, root, ids.as_mut_ptr(), records.as_mut_ptr(), 3, &mut count),
            layout_status::OK
        );
        assert_eq!(count, 3);
        assert_eq!(ids, [root, a, b]);
        assert_eq!(records[1], a_layout);
        assert_eq!(records[2], b_layout);

        // Capacity too small reports the needed count without copying.
        let mut count = 0;
        assert_eq!(
            dump_body(engine, root, ids.as_mut_ptr(), records.as_mut_ptr(), 2, &mut count),
            layout_status::ERR_CAPACITY
        );
        assert_eq!(count, 3);

        // Removing a subtree invalidates its handles and dirties the root.
        assert_eq!(node_remove_subtree_body(engine, a), layout_status::OK);
        let mut stale = blank_layout_record();
        assert_eq!(
            node_layout_body(engine, a, &mut stale),
            layout_status::ERR_STALE_HANDLE
        );
        let mut count = 0;
        assert_eq!(node_child_count_body(engine, root, &mut count), layout_status::OK);
        assert_eq!(count, 1);

        // Available-space record validation.
        let bad_avail = GpuiAvailSize {
            width: GpuiAvail { tag: 9, bits: 0 },
            height: GpuiAvail { tag: 0, bits: 0 },
        };
        let mut flags = 0;
        assert_eq!(
            compute_body(engine, root, &bad_avail, 0, &mut flags),
            layout_status::ERR_BAD_VALUE
        );
    }

    /// Trampoline tests serialize on the process-global trampoline.
    fn with_trampoline(
        trampoline: GpuiGoLayoutTrampoline,
        body: impl FnOnce(),
    ) {
        let _guard = TRAMPOLINE_TEST_LOCK.lock().unwrap_or_else(|poisoned| poisoned.into_inner());
        let previous = TRAMPOLINE.load(Ordering::Acquire);
        TRAMPOLINE.store(trampoline as usize, Ordering::Release);
        body();
        TRAMPOLINE.store(previous, Ordering::Release);
    }

    unsafe extern "system" fn fixed_size_trampoline(
        _request: *mut GpuiMeasureRequest,
        response: *mut GpuiMeasureResponse,
    ) -> i32 {
        unsafe {
            (*response).status = 0;
            (*response).width_bits = 80.0_f32.to_bits();
            (*response).height_bits = 40.0_f32.to_bits();
        }
        0
    }

    unsafe extern "system" fn failing_trampoline(
        _request: *mut GpuiMeasureRequest,
        response: *mut GpuiMeasureResponse,
    ) -> i32 {
        unsafe {
            (*response).status = 1;
            (*response).width_bits = 0;
            (*response).height_bits = 0;
        }
        1
    }

    unsafe extern "system" fn same_engine_reentry_trampoline(
        request: *mut GpuiMeasureRequest,
        response: *mut GpuiMeasureResponse,
    ) -> i32 {
        unsafe {
            let engine = (*request).engine;
            let root = (*request).node;
            // Same-engine nested compute from inside the callback: the busy
            // flag must reject it before the engine state lock.
            let available = definite_avail(300.0, 200.0);
            let mut flags = 0;
            let code = compute_body(engine, root, &available, 0, &mut flags);
            assert_eq!(
                code, layout_status::ERR_ENGINE_BUSY,
                "nested same-engine compute must be rejected"
            );
            // Independent-engine nested compute is allowed.
            let other = create_engine();
            let mut other_record = minimal_record();
            other_record.size = GpuiSizeL { width: px(50.0), height: px(60.0) };
            let other_root = create_node(other, &other_record);
            let (code, _) = compute(other, other_root, definite_avail(50.0, 60.0));
            assert_eq!(code, layout_status::OK);
            assert_eq!(engine_dispose_body(other), layout_status::OK);

            (*response).status = 0;
            (*response).width_bits = 80.0_f32.to_bits();
            (*response).height_bits = 40.0_f32.to_bits();
        }
        0
    }

    #[test]
    fn measure_closure_honors_trampoline_size() {
        with_trampoline(fixed_size_trampoline, || {
            let engine = create_engine();
            let mut root_record = minimal_record();
            root_record.size = GpuiSizeL { width: px(300.0), height: px(200.0) };
            root_record.align_items_present = 1;
            root_record.align_items = 2; // flex-start (no stretch)

            let measured = minimal_record();
            let measured_node = create_node(engine, &measured);
            assert_eq!(
                node_set_measure_body(engine, measured_node, 0xCA11),
                layout_status::OK
            );
            let mut fixed = minimal_record();
            fixed.size = GpuiSizeL { width: px(100.0), height: px(50.0) };
            let fixed_node = create_node(engine, &fixed);
            let root =
                create_node_with_children(engine, &root_record, &[measured_node, fixed_node]);

            let (code, flags) = compute(engine, root, definite_avail(300.0, 200.0));
            assert_eq!(code, layout_status::OK);
            assert_eq!(flags & 1, 1, "measured callbacks were invoked");

            let measured_layout = node_layout(engine, measured_node);
            assert_eq!(
                (measured_layout.location_x, measured_layout.location_y),
                (0.0, 0.0)
            );
            assert_eq!((measured_layout.size_w, measured_layout.size_h), (80.0, 40.0));
            let fixed_layout = node_layout(engine, fixed_node);
            assert_eq!((fixed_layout.location_x, fixed_layout.location_y), (80.0, 0.0));
            assert_eq!((fixed_layout.size_w, fixed_layout.size_h), (100.0, 50.0));
        });
    }

    #[test]
    fn measure_without_trampoline_is_a_caller_error() {
        let _guard = TRAMPOLINE_TEST_LOCK.lock().unwrap_or_else(|poisoned| poisoned.into_inner());
        let previous = TRAMPOLINE.load(Ordering::Acquire);
        TRAMPOLINE.store(0, Ordering::Release);

        let engine = create_engine();
        let mut root_record = minimal_record();
        root_record.size = GpuiSizeL { width: px(300.0), height: px(200.0) };
        let measured_node = create_node(engine, &minimal_record());
        node_set_measure_body(engine, measured_node, 0xCA11);
        let root = create_node_with_children(engine, &root_record, &[measured_node]);
        let (code, flags) = compute(engine, root, definite_avail(300.0, 200.0));
        assert_eq!(code, layout_status::ERR_NO_TRAMPOLINE);
        assert_eq!(flags & 1, 0);

        TRAMPOLINE.store(previous, Ordering::Release);
    }

    #[test]
    fn trampoline_failure_latches_and_fails_the_engine() {
        with_trampoline(failing_trampoline, || {
            let engine = create_engine();
            let mut root_record = minimal_record();
            root_record.size = GpuiSizeL { width: px(300.0), height: px(200.0) };
            let measured_node = create_node(engine, &minimal_record());
            node_set_measure_body(engine, measured_node, 0xCA11);
            let root = create_node_with_children(engine, &root_record, &[measured_node]);

            let (code, flags) = compute(engine, root, definite_avail(300.0, 200.0));
            assert_eq!(code, layout_status::ERR_CALLBACK);
            assert_eq!(flags & 1, 1);

            // The engine is marked failed until reset.
            let mut record = blank_layout_record();
            assert_eq!(
                node_layout_body(engine, root, &mut record),
                layout_status::ERR_ENGINE_FAILED
            );
            assert_eq!(
                compute(engine, root, definite_avail(300.0, 200.0)).0,
                layout_status::ERR_ENGINE_FAILED
            );

            // Reset clears the failed state.
            assert_eq!(engine_reset_body(engine), layout_status::OK);
            let fresh = create_node(engine, &minimal_record());
            let mut record = blank_layout_record();
            assert_eq!(node_layout_body(engine, fresh, &mut record), layout_status::OK);
        });
    }

    #[test]
    fn nested_same_engine_compute_is_rejected_not_deadlocked() {
        with_trampoline(same_engine_reentry_trampoline, || {
            let engine = create_engine();
            let mut root_record = minimal_record();
            root_record.size = GpuiSizeL { width: px(300.0), height: px(200.0) };
            root_record.align_items_present = 1;
            root_record.align_items = 2; // flex-start: no cross-axis stretch
            let measured_node = create_node(engine, &minimal_record());
            node_set_measure_body(engine, measured_node, 0xCA11);
            let root = create_node_with_children(engine, &root_record, &[measured_node]);

            let (code, flags) = compute(engine, root, definite_avail(300.0, 200.0));
            assert_eq!(code, layout_status::OK, "the outer compute succeeds");
            assert_eq!(flags & 1, 1);
            let layout = node_layout(engine, measured_node);
            assert_eq!((layout.size_w, layout.size_h), (80.0, 40.0));
        });
    }
}
