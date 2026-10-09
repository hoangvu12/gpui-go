//! gpui-go native image service (ticket17, reserved slot 7).
//!
//! The pinned image codec surface of the gpui-CE reference: the `image`
//! crate (workspace `image = "0.25.1"`, lock resolution 0.25.10 — the
//! same graph as the pinned checkout) decoding CPU images into BGRA
//! frames with per-frame rational delays, exactly the paths
//! `crates/gpui/src/elements/img.rs` (the resource entry:
//! `image::guess_format` sniffing, the GIF and animated-WebP frame
//! walks with bad frames skipped, the static WebP branch, the SVG
//! fallback signal), `crates/gpui/src/platform.rs`
//! (`decode_static_image`/`decode_static_image_from_decoder`: explicit
//! format, EXIF orientation applied, DynamicImage → rgba8 → the
//! RGBA→BGRA channel swap, `Frame::new`) and
//! `crates/gpui_windows/src/clipboard.rs` (the clipboard entry: the
//! format is KNOWN from the clipboard's registered format, never
//! sniffed; CF_DIB is converted to BMP before the decode) use.
//!
//! ABI shape (see `IMAGE_ABI.md` and the crate docs): fixed-width
//! `#[repr(C)]` records, no Rust slices/strings/enums crossing the
//! boundary, caller-owned buffers with the capacity protocol, every
//! export containing panics with `catch_unwind`, and a slot registry
//! for live decoded images mirroring the text service's shaping-handle
//! registry (handle = `(slot index << 20) | slot generation`, stale
//! after dispose or slot reuse, capacity [`MAX_IMAGE_SLOTS`]).
//!
//! Entry modes (the ticket's explicit distinction):
//!
//! * **Resource** (`image_decode_resource`): `image::guess_format`
//!   sniffs the format from the bytes — never an extension list — then
//!   the pinned branch: GIF and animated WebP walk frames (bad frames
//!   skipped, all-failed is a typed error), static WebP and every other
//!   supported format decode statically with EXIF orientation. An
//!   unknown sniff is [`image_status::ERR_FORMAT_UNKNOWN`] so the caller
//!   can route to the SVG renderer exactly like the pin's `img.rs`.
//! * **Clipboard** (`image_decode_clipboard`): the format tag is known
//!   (PNG/JPEG/GIF/BMP from the clipboard's registered formats; DIB was
//!   already converted to BMP bytes by the caller). GIF and WebP use the
//!   animated walks (clipboard images render through the same decode
//!   path in the pin); the static formats decode with
//!   `ImageReader::with_format`, matching `decode_static_image`.
//!
//! Output frames are BGRA8 rows (the pin swaps RGBA→BGRA everywhere)
//! with `(width, height)` and the image crate's rational
//! millisecond delay per frame; a static decode is one frame with the
//! `Frame::new` zero delay.

use std::io::Cursor;
use std::panic::{catch_unwind, AssertUnwindSafe};
use std::sync::{LazyLock, Mutex, MutexGuard};

use image::codecs::{gif::GifDecoder, webp::WebPDecoder};
use image::{AnimationDecoder as _, DynamicImage, ImageDecoder, ImageFormat, Rgba};

// ---------------------------------------------------------------------------
// Constants, status codes, format tags
// ---------------------------------------------------------------------------

/// Live decoded-image slot bound (the text service's shaping-slot
/// bound, mirrored).
pub const MAX_IMAGE_SLOTS: u32 = 256;

/// Maximum accepted encoded-image byte length (caller memory is copied
/// into the decode; the bound keeps the native work bounded).
pub const MAX_IMAGE_BYTES: u32 = 1 << 28;

/// Maximum frames one decode may retain (an animated bomb guard; the
/// pin has no bound, the port records the deviation).
pub const MAX_FRAMES: u32 = 4096;

/// Image service status codes. 0 is success; negative values are
/// caller/argument errors; 100+ are internal failures.
pub mod image_status {
    /// Success.
    pub const OK: i32 = 0;
    /// The image handle encoded a past generation (disposed or slot
    /// reused since).
    pub const ERR_STALE_HANDLE: i32 = -1;
    /// The handle never existed or is malformed (out-of-range slot or a
    /// never-issued generation).
    pub const ERR_BAD_HANDLE: i32 = -2;
    /// A required pointer argument was null.
    pub const ERR_NULL_ARG: i32 = -3;
    /// An invalid length, capacity, format tag or frame index.
    pub const ERR_BAD_VALUE: i32 = -4;
    /// The resource-entry sniff could not recognize the bytes (the pin
    /// routes these to the SVG renderer; the caller does the same).
    pub const ERR_FORMAT_UNKNOWN: i32 = -5;
    /// The decoder rejected the bytes (creation or read failure).
    pub const ERR_DECODE: i32 = -6;
    /// An animated decode produced no usable frame (every frame failed;
    /// the pinned all-frames-failed error).
    pub const ERR_ALL_FRAMES_FAILED: i32 = -7;
    /// A dump capacity was too small; `out_needed` carries the required
    /// byte count.
    pub const ERR_CAPACITY: i32 = -8;
    /// The live image-handle capacity ([`MAX_IMAGE_SLOTS`]) is
    /// exhausted.
    pub const ERR_IMAGE_LIMIT: i32 = -9;
    /// A native panic was contained by `catch_unwind` during this call.
    pub const ERR_PANIC: i32 = 101;
}

/// Image format tags (u32 on the ABI; one per pinned `ImageFormat`
/// discriminant the codec graph supports, plus the SVG tag that only
/// the probe reports so the caller can route like the pin).
pub mod image_format {
    pub const PNG: u32 = 1;
    pub const JPEG: u32 = 2;
    pub const GIF: u32 = 3;
    pub const WEBP: u32 = 4;
    pub const BMP: u32 = 5;
    pub const TIFF: u32 = 6;
    pub const ICO: u32 = 7;
    pub const PNM: u32 = 8;
    /// Reported by the probe only; decode rejects it with
    /// [`image_status::ERR_FORMAT_UNKNOWN`] (the SVG renderer is a
    /// separate native service per the deferred ticket).
    pub const SVG: u32 = 9;
}

/// Maps a pinned `ImageFormat` to its ABI tag.
fn format_tag(format: ImageFormat) -> Option<u32> {
    // The image crate has no SVG variant: guess_format fails for SVG
    // bytes, and that failure is exactly how the pin routes to the SVG
    // renderer (the ABI's SVG tag exists only for the probe/clipboard
    // entry rejection).
    Some(match format {
        ImageFormat::Png => image_format::PNG,
        ImageFormat::Jpeg => image_format::JPEG,
        ImageFormat::Gif => image_format::GIF,
        ImageFormat::WebP => image_format::WEBP,
        ImageFormat::Bmp => image_format::BMP,
        ImageFormat::Tiff => image_format::TIFF,
        ImageFormat::Ico => image_format::ICO,
        ImageFormat::Pnm => image_format::PNM,
        _ => return None,
    })
}

/// Maps an ABI tag to the pinned `ImageFormat`.
fn pinned_format(tag: u32) -> Option<ImageFormat> {
    Some(match tag {
        image_format::PNG => ImageFormat::Png,
        image_format::JPEG => ImageFormat::Jpeg,
        image_format::GIF => ImageFormat::Gif,
        image_format::WEBP => ImageFormat::WebP,
        image_format::BMP => ImageFormat::Bmp,
        image_format::TIFF => ImageFormat::Tiff,
        image_format::ICO => ImageFormat::Ico,
        image_format::PNM => ImageFormat::Pnm,
        _ => return None,
    })
}

// ---------------------------------------------------------------------------
// Records
// ---------------------------------------------------------------------------

/// One decoded frame's geometry and delay (caller-owned, written by
/// `image_frame_info`).
///
/// Layout (x86-64, `#[repr(C)`]): `width` @0, `height` @4,
/// `delay_numer_ms` @8, `delay_denom_ms` @12, `reserved` @16; size 20,
/// alignment 4.
#[repr(C)]
pub struct GpuiGoImageFrameRecord {
    /// Frame width in pixels.
    pub width: u32,
    /// Frame height in pixels.
    pub height: u32,
    /// The frame delay's numerator (milliseconds; the image crate's
    /// rational). Zero for a static decode's `Frame::new`.
    pub delay_numer_ms: u32,
    /// The frame delay's denominator (milliseconds).
    pub delay_denom_ms: u32,
    /// Reserved (zero).
    pub reserved: u32,
}

/// One codec-graph row (caller-owned, written by `image_codec_graph`):
/// the format tag and whether the pinned graph animates it.
///
/// Layout: `format_tag` @0, `animated` @4, `reserved` @8; size 12,
/// alignment 4.
#[repr(C)]
pub struct GpuiGoImageCodecRecord {
    /// The ABI format tag.
    pub format_tag: u32,
    /// 1 when the pinned graph decodes this format through the animated
    /// frame walk (GIF, WebP); 0 for the static path.
    pub animated: u32,
    /// Reserved (zero).
    pub reserved: u32,
}

// ---------------------------------------------------------------------------
// Slot registry
// ---------------------------------------------------------------------------

/// One decoded frame in service-owned memory.
struct DecodedFrame {
    width: u32,
    height: u32,
    bgra: Vec<u8>,
    delay_numer_ms: u32,
    delay_denom_ms: u32,
}

/// One decoded image occupying a slot.
struct DecodedImage {
    frames: Vec<DecodedFrame>,
}

/// A registry slot (the text service's `ShapingSlot` shape).
struct ImageSlot {
    /// Slot generation of the last image that occupied this slot
    /// (persists while vacant so disposed handles stay stale after slot
    /// reuse).
    slot_generation: u32,
    occupied: bool,
    image: Option<DecodedImage>,
}

impl ImageSlot {
    fn vacant() -> Self {
        ImageSlot { slot_generation: 0, occupied: false, image: None }
    }
}

/// The whole image service state behind one mutex (the serialization
/// boundary; no lock is held across any callback — the service has
/// none).
struct ImageServiceState {
    slots: Vec<ImageSlot>,
}

static GLOBAL: LazyLock<Mutex<ImageServiceState>> =
    LazyLock::new(|| Mutex::new(ImageServiceState { slots: Vec::new() }));

fn lock_global() -> MutexGuard<'static, ImageServiceState> {
    GLOBAL.lock().unwrap_or_else(|poisoned| poisoned.into_inner())
}

/// Generation ceiling (the handle encoding's 20-bit generation space).
const IMAGE_GENERATION_MAX: u32 = (1 << 20) - 1;

/// Splits a handle into (slot index, slot generation).
fn image_handle_parts(handle: u64) -> (usize, u32) {
    ((handle >> 20) as usize, (handle & 0xFFFFF) as u32)
}

/// Encodes a handle from (slot index, slot generation).
fn make_image_handle(slot: usize, generation: u32) -> u64 {
    ((slot as u64) << 20) | generation as u64
}

// ---------------------------------------------------------------------------
// The pinned decode paths
// ---------------------------------------------------------------------------

/// The pin's `decode_static_image_from_decoder`: orientation →
/// DynamicImage → rgba8 → the RGBA→BGRA swap, one frame.
fn decode_static_from_decoder(
    mut decoder: impl ImageDecoder,
) -> Result<DecodedImage, i32> {
    let orientation = decoder
        .orientation()
        .map_err(|_| image_status::ERR_DECODE)?;
    let mut image = DynamicImage::from_decoder(decoder).map_err(|_| image_status::ERR_DECODE)?;
    image.apply_orientation(orientation);

    let mut data = image.into_rgba8();
    for pixel in data.chunks_exact_mut(4) {
        pixel.swap(0, 2);
    }
    let (width, height) = data.dimensions();
    Ok(DecodedImage {
        frames: vec![DecodedFrame {
            width,
            height,
            bgra: data.into_raw(),
            // The pin's `Frame::new` delay: zero.
            delay_numer_ms: 0,
            delay_denom_ms: 0,
        }],
    })
}

/// The pin's `decode_static_image`: `ImageReader::with_format` →
/// `into_decoder` → [`decode_static_from_decoder`].
fn decode_static(bytes: &[u8], format: ImageFormat) -> Result<DecodedImage, i32> {
    let decoder = image::ImageReader::with_format(Cursor::new(bytes), format)
        .into_decoder()
        .map_err(|_| image_status::ERR_DECODE)?;
    decode_static_from_decoder(decoder)
}

/// The pin's animated frame walk (GIF and animated WebP in `img.rs`):
/// iterate frames, swap RGBA→BGRA in place, keep each frame's rational
/// delay, skip decode-error frames, and fail typed when nothing
/// survived. Orientation is NOT applied on this path (the pin does
/// not).
fn decode_animated_frames<'a, I>(frames: I) -> Result<DecodedImage, i32>
where
    I: Iterator<Item = Result<image::Frame, image::ImageError>> + 'a,
{
    let mut decoded: Vec<DecodedFrame> = Vec::new();
    for frame in frames {
        let mut frame = match frame {
            Ok(frame) => frame,
            // The pin logs and skips the frame.
            Err(_) => continue,
        };
        // Convert RGBA to BGRA (the pinned swap).
        for pixel in frame.buffer_mut().chunks_exact_mut(4) {
            pixel.swap(0, 2);
        }
        let (numer, denom) = frame.delay().numer_denom_ms();
        let (width, height) = frame.buffer().dimensions();
        if decoded.len() >= MAX_FRAMES as usize {
            break;
        }
        decoded.push(DecodedFrame {
            width,
            height,
            bgra: frame.into_buffer().into_raw(),
            delay_numer_ms: numer as u32,
            delay_denom_ms: denom as u32,
        });
    }
    if decoded.is_empty() {
        return Err(image_status::ERR_ALL_FRAMES_FAILED);
    }
    Ok(DecodedImage { frames: decoded })
}

/// The pinned resource-entry branch table: sniffed format → the
/// GIF/WebP/static dispatch of `img.rs`'s load path.
fn decode_sniffed(bytes: &[u8]) -> Result<DecodedImage, i32> {
    let format =
        image::guess_format(bytes).map_err(|_| image_status::ERR_FORMAT_UNKNOWN)?;
    decode_with_format(bytes, format)
}

/// The format-keyed dispatch (shared by both entry modes): GIF and WebP
/// take the animated walks, everything else decodes statically with
/// orientation.
fn decode_with_format(bytes: &[u8], format: ImageFormat) -> Result<DecodedImage, i32> {
    match format {
        ImageFormat::Gif => {
            let decoder = GifDecoder::new(Cursor::new(bytes)).map_err(|_| image_status::ERR_DECODE)?;
            decode_animated_frames(decoder.into_frames())
        }
        ImageFormat::WebP => {
            let mut decoder =
                WebPDecoder::new(Cursor::new(bytes)).map_err(|_| image_status::ERR_DECODE)?;
            if decoder.has_animation() {
                // The pinned background reset (its result is ignored).
                let _ = decoder.set_background_color(Rgba([0, 0, 0, 0]));
                decode_animated_frames(decoder.into_frames())
            } else {
                decode_static_from_decoder(decoder)
            }
        }
        _ => decode_static(bytes, format),
    }
}

// ---------------------------------------------------------------------------
// Entry bodies
// ---------------------------------------------------------------------------

/// Registers a decoded image in a free slot, returning the handle.
fn register_image(state: &mut ImageServiceState, image: DecodedImage) -> Result<u64, i32> {
    let slot_index = match state.slots.iter().position(|slot| !slot.occupied) {
        Some(index) => index,
        None if state.slots.len() < MAX_IMAGE_SLOTS as usize => {
            state.slots.push(ImageSlot::vacant());
            state.slots.len() - 1
        }
        None => return Err(image_status::ERR_IMAGE_LIMIT),
    };
    let slot_generation = match state.slots[slot_index].slot_generation.checked_add(1) {
        Some(generation) if generation <= IMAGE_GENERATION_MAX => generation,
        _ => return Err(image_status::ERR_BAD_VALUE),
    };
    state.slots[slot_index] = ImageSlot {
        slot_generation,
        occupied: true,
        image: Some(image),
    };
    Ok(make_image_handle(slot_index, slot_generation))
}

/// Validates a borrowed byte slice (null-vs-length, bound).
fn borrowed_bytes(ptr: *const u8, len: u32) -> Result<&'static [u8], i32> {
    if ptr.is_null() {
        return if len == 0 { Ok(&[]) } else { Err(image_status::ERR_NULL_ARG) };
    }
    if len > MAX_IMAGE_BYTES {
        return Err(image_status::ERR_BAD_VALUE);
    }
    Ok(unsafe { std::slice::from_raw_parts(ptr, len as usize) })
}

fn decode_resource_body(bytes: *const u8, len: u32, out_handle: *mut u64) -> i32 {
    if out_handle.is_null() {
        return image_status::ERR_NULL_ARG;
    }
    let bytes = match borrowed_bytes(bytes, len) {
        Ok(bytes) => bytes,
        Err(code) => return code,
    };
    if bytes.is_empty() {
        return image_status::ERR_FORMAT_UNKNOWN;
    }
    let image = match decode_sniffed(bytes) {
        Ok(image) => image,
        Err(code) => return code,
    };
    let mut state = lock_global();
    match register_image(&mut state, image) {
        Ok(handle) => {
            unsafe { *out_handle = handle };
            image_status::OK
        }
        Err(code) => code,
    }
}

fn decode_clipboard_body(
    bytes: *const u8,
    len: u32,
    format_tag: u32,
    out_handle: *mut u64,
) -> i32 {
    if out_handle.is_null() {
        return image_status::ERR_NULL_ARG;
    }
    let format = match pinned_format(format_tag) {
        Some(format) if format_tag != image_format::SVG => format,
        _ => return image_status::ERR_BAD_VALUE,
    };
    let bytes = match borrowed_bytes(bytes, len) {
        Ok(bytes) => bytes,
        Err(code) => return code,
    };
    if bytes.is_empty() {
        return image_status::ERR_DECODE;
    }
    let image = match decode_with_format(bytes, format) {
        Ok(image) => image,
        Err(code) => return code,
    };
    let mut state = lock_global();
    match register_image(&mut state, image) {
        Ok(handle) => {
            unsafe { *out_handle = handle };
            image_status::OK
        }
        Err(code) => code,
    }
}

fn format_probe_body(bytes: *const u8, len: u32, out_format: *mut u32) -> i32 {
    if out_format.is_null() {
        return image_status::ERR_NULL_ARG;
    }
    let bytes = match borrowed_bytes(bytes, len) {
        Ok(bytes) => bytes,
        Err(code) => return code,
    };
    if bytes.is_empty() {
        return image_status::ERR_FORMAT_UNKNOWN;
    }
    match image::guess_format(bytes) {
        Ok(format) => match format_tag(format) {
            Some(tag) => {
                unsafe { *out_format = tag };
                image_status::OK
            }
            None => image_status::ERR_FORMAT_UNKNOWN,
        },
        Err(_) => image_status::ERR_FORMAT_UNKNOWN,
    }
}

/// Resolves a handle to its occupying slot (typed stale/bad errors).
fn resolve_slot(
    state: &ImageServiceState,
    handle: u64,
) -> Result<usize, i32> {
    let (slot_index, slot_generation) = image_handle_parts(handle);
    if slot_index >= state.slots.len() {
        return Err(image_status::ERR_BAD_HANDLE);
    }
    let slot = &state.slots[slot_index];
    if !slot.occupied || slot.slot_generation != slot_generation {
        return Err(image_status::ERR_STALE_HANDLE);
    }
    Ok(slot_index)
}

fn frame_count_body(handle: u64, out_count: *mut u32) -> i32 {
    if out_count.is_null() {
        return image_status::ERR_NULL_ARG;
    }
    let state = lock_global();
    match resolve_slot(&state, handle) {
        Ok(slot_index) => {
            let count =
                state.slots[slot_index].image.as_ref().map(|i| i.frames.len()).unwrap_or(0) as u32;
            unsafe { *out_count = count };
            image_status::OK
        }
        Err(code) => code,
    }
}

fn frame_info_body(handle: u64, frame_index: u32, out: *mut GpuiGoImageFrameRecord) -> i32 {
    if out.is_null() {
        return image_status::ERR_NULL_ARG;
    }
    let state = lock_global();
    let slot_index = match resolve_slot(&state, handle) {
        Ok(index) => index,
        Err(code) => return code,
    };
    let image = state.slots[slot_index].image.as_ref().unwrap();
    let frame = match image.frames.get(frame_index as usize) {
        Some(frame) => frame,
        None => return image_status::ERR_BAD_VALUE,
    };
    unsafe {
        *out = GpuiGoImageFrameRecord {
            width: frame.width,
            height: frame.height,
            delay_numer_ms: frame.delay_numer_ms,
            delay_denom_ms: frame.delay_denom_ms,
            reserved: 0,
        };
    }
    image_status::OK
}

fn frame_pixels_body(
    handle: u64,
    frame_index: u32,
    buf: *mut u8,
    capacity: u32,
    out_needed: *mut u32,
) -> i32 {
    if out_needed.is_null() {
        return image_status::ERR_NULL_ARG;
    }
    let state = lock_global();
    let slot_index = match resolve_slot(&state, handle) {
        Ok(index) => index,
        Err(code) => return code,
    };
    let image = state.slots[slot_index].image.as_ref().unwrap();
    let frame = match image.frames.get(frame_index as usize) {
        Some(frame) => frame,
        None => return image_status::ERR_BAD_VALUE,
    };
    let needed = frame.bgra.len() as u32;
    unsafe { *out_needed = needed };
    if needed == 0 {
        return image_status::OK;
    }
    if buf.is_null() || capacity < needed {
        return image_status::ERR_CAPACITY;
    }
    unsafe {
        std::ptr::copy_nonoverlapping(frame.bgra.as_ptr(), buf, needed as usize);
    }
    image_status::OK
}

fn dispose_body(handle: u64) -> i32 {
    let mut state = lock_global();
    let (slot_index, slot_generation) = image_handle_parts(handle);
    if slot_index >= state.slots.len() {
        return image_status::ERR_BAD_HANDLE;
    }
    let slot = &mut state.slots[slot_index];
    if !slot.occupied || slot.slot_generation != slot_generation {
        return image_status::ERR_STALE_HANDLE;
    }
    slot.occupied = false;
    slot.image = None;
    image_status::OK
}

/// The effective codec graph: the pinned formats this build supports
/// and which take the animated walk.
fn codec_graph_rows() -> Vec<GpuiGoImageCodecRecord> {
    // The pinned graph's animation split (img.rs): GIF and WebP animate;
    // the rest are static. Every format the tag space names (except SVG,
    // the deferred renderer) is supported by the crate's default features.
    vec![
        GpuiGoImageCodecRecord { format_tag: image_format::PNG, animated: 0, reserved: 0 },
        GpuiGoImageCodecRecord { format_tag: image_format::JPEG, animated: 0, reserved: 0 },
        GpuiGoImageCodecRecord { format_tag: image_format::GIF, animated: 1, reserved: 0 },
        GpuiGoImageCodecRecord { format_tag: image_format::WEBP, animated: 1, reserved: 0 },
        GpuiGoImageCodecRecord { format_tag: image_format::BMP, animated: 0, reserved: 0 },
        GpuiGoImageCodecRecord { format_tag: image_format::TIFF, animated: 0, reserved: 0 },
        GpuiGoImageCodecRecord { format_tag: image_format::ICO, animated: 0, reserved: 0 },
        GpuiGoImageCodecRecord { format_tag: image_format::PNM, animated: 0, reserved: 0 },
    ]
}

fn codec_graph_body(
    records: *mut GpuiGoImageCodecRecord,
    capacity: u32,
    out_count: *mut u32,
    out_needed: *mut u32,
) -> i32 {
    if out_needed.is_null() || out_count.is_null() {
        return image_status::ERR_NULL_ARG;
    }
    let rows = codec_graph_rows();
    let needed = rows.len() as u32;
    unsafe {
        *out_needed = needed;
        *out_count = 0;
    }
    if needed == 0 {
        return image_status::OK;
    }
    if records.is_null() || capacity < needed {
        return image_status::ERR_CAPACITY;
    }
    unsafe {
        for (i, row) in rows.iter().enumerate() {
            *records.add(i) = GpuiGoImageCodecRecord {
                format_tag: row.format_tag,
                animated: row.animated,
                reserved: 0,
            };
        }
        *out_count = needed;
    }
    image_status::OK
}

// ---------------------------------------------------------------------------
// Panic containment and exports
// ---------------------------------------------------------------------------

macro_rules! image_entry {
    ($body:expr) => {{
        let outcome = catch_unwind(AssertUnwindSafe(|| $body));
        match outcome {
            Ok(code) => code,
            Err(payload) => {
                // Drop the payload; nothing is retained. The state mutex
                // recovers from poisoning on the next lock (no callback can
                // run under it, so no partial state survives).
                drop(payload);
                image_status::ERR_PANIC
            }
        }
    }};
}

unsafe extern "system" fn image_decode_resource(
    bytes: *const u8,
    len: u32,
    out_handle: *mut u64,
) -> i32 {
    image_entry!(decode_resource_body(bytes, len, out_handle))
}

unsafe extern "system" fn image_decode_clipboard(
    bytes: *const u8,
    len: u32,
    format_tag: u32,
    out_handle: *mut u64,
) -> i32 {
    image_entry!(decode_clipboard_body(bytes, len, format_tag, out_handle))
}

unsafe extern "system" fn image_format_probe(
    bytes: *const u8,
    len: u32,
    out_format: *mut u32,
) -> i32 {
    image_entry!(format_probe_body(bytes, len, out_format))
}

unsafe extern "system" fn image_frame_count(
    handle: u64,
    out_count: *mut u32,
) -> i32 {
    image_entry!(frame_count_body(handle, out_count))
}

unsafe extern "system" fn image_frame_info(
    handle: u64,
    frame_index: u32,
    out: *mut GpuiGoImageFrameRecord,
) -> i32 {
    image_entry!(frame_info_body(handle, frame_index, out))
}

unsafe extern "system" fn image_frame_pixels(
    handle: u64,
    frame_index: u32,
    buf: *mut u8,
    capacity: u32,
    out_needed: *mut u32,
) -> i32 {
    image_entry!(frame_pixels_body(handle, frame_index, buf, capacity, out_needed))
}

unsafe extern "system" fn image_dispose(handle: u64) -> i32 {
    image_entry!(dispose_body(handle))
}

unsafe extern "system" fn image_codec_graph(
    records: *mut GpuiGoImageCodecRecord,
    capacity: u32,
    out_count: *mut u32,
    out_needed: *mut u32,
) -> i32 {
    image_entry!(codec_graph_body(records, capacity, out_count, out_needed))
}

// ---------------------------------------------------------------------------
// The service table
// ---------------------------------------------------------------------------

/// The image service table: 8 function pointers then 17 self-check,
/// capacity and format-tag scalars; size 136, alignment 8. Installed in reserved slot
/// 7 of the bootstrap table and advertised with capability bit 8
/// (`image-codecs-image-0-25`).
#[repr(C)]
pub struct GpuiGoImageTable {
    /// `image_decode_resource` (the sniffed resource entry).
    pub decode_resource:
        Option<unsafe extern "system" fn(*const u8, u32, *mut u64) -> i32>,
    /// `image_decode_clipboard` (the known-format clipboard entry).
    pub decode_clipboard:
        Option<unsafe extern "system" fn(*const u8, u32, u32, *mut u64) -> i32>,
    /// `image_format_probe` (`image::guess_format` sniffing).
    pub format_probe:
        Option<unsafe extern "system" fn(*const u8, u32, *mut u32) -> i32>,
    /// `image_frame_count`.
    pub frame_count: Option<unsafe extern "system" fn(u64, *mut u32) -> i32>,
    /// `image_frame_info`.
    pub frame_info:
        Option<unsafe extern "system" fn(u64, u32, *mut GpuiGoImageFrameRecord) -> i32>,
    /// `image_frame_pixels` (BGRA8 bytes, capacity protocol).
    pub frame_pixels:
        Option<unsafe extern "system" fn(u64, u32, *mut u8, u32, *mut u32) -> i32>,
    /// `image_dispose`.
    pub dispose: Option<unsafe extern "system" fn(u64) -> i32>,
    /// `image_codec_graph` (the effective codec list).
    pub codec_graph: Option<
        unsafe extern "system" fn(
            *mut GpuiGoImageCodecRecord,
            u32,
            *mut u32,
            *mut u32,
        ) -> i32,
    >,
    /// Image service ABI version (1).
    pub service_version: u32,
    /// `size_of::<GpuiGoImageTable>()` self-check.
    pub size_of_table: u32,
    /// `align_of::<GpuiGoImageTable>()` self-check.
    pub align_of_table: u32,
    /// `size_of::<GpuiGoImageFrameRecord>()` self-check.
    pub size_of_frame_record: u32,
    /// `size_of::<GpuiGoImageCodecRecord>()` self-check.
    pub size_of_codec_record: u32,
    /// Live image-handle bound ([`MAX_IMAGE_SLOTS`]).
    pub max_image_handles: u32,
    /// Maximum accepted encoded-image bytes ([`MAX_IMAGE_BYTES`]).
    pub max_image_bytes: u32,
    /// Maximum frames per decode ([`MAX_FRAMES`]).
    pub max_frames: u32,
    /// The PNG format tag.
    pub format_png: u32,
    /// The JPEG format tag.
    pub format_jpeg: u32,
    /// The GIF format tag.
    pub format_gif: u32,
    /// The WebP format tag.
    pub format_webp: u32,
    /// The BMP format tag.
    pub format_bmp: u32,
    /// The TIFF format tag.
    pub format_tiff: u32,
    /// The ICO format tag.
    pub format_ico: u32,
    /// The PNM format tag.
    pub format_pnm: u32,
    /// The SVG tag (probe-reported only; decode rejects it).
    pub format_svg: u32,
}

/// Image service ABI version.
pub const GPUI_GO_IMAGE_SERVICE_VERSION: u32 = 1;

/// The one static image service table.
pub static IMAGE_TABLE: GpuiGoImageTable = GpuiGoImageTable {
    decode_resource: Some(image_decode_resource),
    decode_clipboard: Some(image_decode_clipboard),
    format_probe: Some(image_format_probe),
    frame_count: Some(image_frame_count),
    frame_info: Some(image_frame_info),
    frame_pixels: Some(image_frame_pixels),
    dispose: Some(image_dispose),
    codec_graph: Some(image_codec_graph),
    service_version: GPUI_GO_IMAGE_SERVICE_VERSION,
    size_of_table: std::mem::size_of::<GpuiGoImageTable>() as u32,
    align_of_table: std::mem::align_of::<GpuiGoImageTable>() as u32,
    size_of_frame_record: std::mem::size_of::<GpuiGoImageFrameRecord>() as u32,
    size_of_codec_record: std::mem::size_of::<GpuiGoImageCodecRecord>() as u32,
    max_image_handles: MAX_IMAGE_SLOTS,
    max_image_bytes: MAX_IMAGE_BYTES,
    max_frames: MAX_FRAMES,
    format_png: image_format::PNG,
    format_jpeg: image_format::JPEG,
    format_gif: image_format::GIF,
    format_webp: image_format::WEBP,
    format_bmp: image_format::BMP,
    format_tiff: image_format::TIFF,
    format_ico: image_format::ICO,
    format_pnm: image_format::PNM,
    format_svg: image_format::SVG,
};

// Compile-time layout pins (the Go loader mirrors these exactly).
const _: () = assert!(std::mem::size_of::<GpuiGoImageTable>() == 136);
const _: () = assert!(std::mem::align_of::<GpuiGoImageTable>() == 8);
const _: () = assert!(std::mem::size_of::<GpuiGoImageFrameRecord>() == 20);
const _: () = assert!(std::mem::size_of::<GpuiGoImageCodecRecord>() == 12);
