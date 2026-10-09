//! gpui-go bootstrap + layout native artifact (tickets 02/06/07/08/09).
//!
//! Maintainer-built cdylib exposing a fixed C ABI service table. Ticket02
//! shipped the dependency-free bootstrap slice: an ABI table plus one
//! checked buffer round trip. Ticket06 added the **layout service**
//! (Taffy 0.13.0, following the pinned gpui-CE adapter semantics) in
//! reserved slot 1 of the same table, behind `crate::layout`. Ticket07
//! added the **renderer service** (D3D11 clear-frame present/retire, the
//! pinned gpui-CE Windows renderer's device/swap-chain semantics) in
//! reserved slot 2, behind `crate::renderer`. Ticket08 added the
//! **scene kernel service** (the pinned gpui-CE scene kernel: bounds-tree
//! draw orders, layers, order floors, finish/sort, batching, filter
//! planning, paired surface opacity, replay) in reserved slot 3, behind
//! `crate::scene`. Ticket09 added the **text geometry service** (the
//! pinned Parley/Fontique text stack: system font enumeration, font
//! resolution, shaping, visual lines, paint fragments, positioned
//! glyphs, carets, selections, hit tests and grapheme clusters) in
//! reserved slot 4, behind `crate::text`. Ticket17 added the **image
//! codec service** (the pinned `image`-crate decode graph: resource
//! sniffing and clipboard known-format entries, EXIF orientation,
//! GIF/animated-WebP frame walks, BGRA output) in reserved slot 7,
//! behind `crate::image`.
//!
//! Contract summary (see `docs/distribution-contract.md` and
//! `reference/native/LAYOUT_ABI.md`):
//!
//! * Fixed-width, `#[repr(C)]` records only; no Rust slices, strings, enums,
//!   booleans, closures or `Vec` cross the ABI.
//! * The DLL retains **nothing** across calls: the caller owns all memory and
//!   the library stays loaded for the process lifetime (no `FreeLibrary`).
//!   (The layout engine registry is engine-scoped state, not caller memory;
//!   no Go pointer is ever retained by it.)
//! * Every export contains panics with [`std::panic::catch_unwind`] before
//!   returning to the foreign caller; a contained panic becomes status 7
//!   (bootstrap) or a layout status (see `layout::layout_status`).
//! * No threads, no network, no allocator use on the round-trip path.
//!
//! Exports (C ABI, `extern "system"`):
//!
//! * [`gpui_go_abi`] — returns a pointer to the static [`GpuiGoAbiTable`].
//! * [`gpui_go_panic_probe`] — test-only export that panics on purpose so the
//!   Go conformance tests can observe real panic containment (status 7).
//!
//! The buffer round trip and the layout service are not exported by name;
//! they are reachable only through the function pointers in the bootstrap
//! table (slot 0's `buffer_round_trip` field and reserved slot 1's layout
//! service table), which is how the loader is required to call them.

pub mod atlas;
pub mod glyph;
pub mod image;
pub mod layout;
pub mod renderer;
pub mod scene;
pub mod text;

use std::mem::{align_of, size_of};
use std::panic::{catch_unwind, AssertUnwindSafe};

/// ABI magic. Read big-endian the bytes spell "GPGO" (`'G' 'P' 'G' 'O'` =
/// `0x47 0x50 0x47 0x4F`); in a little-endian image the in-memory byte order
/// is reversed, as for any numeric constant.
pub const GPUI_GO_ABI_MAGIC: u32 = 0x4750_474F; // "GPGO"

/// Private ABI schema major version of this bootstrap table.
pub const GPUI_GO_ABI_VERSION: u32 = 1;

/// Revision of the native bridge source itself (bumped on any native-side
/// change, independently of `abi_version`). Ticket02 shipped revision 1;
/// ticket06 (layout service) bumped it to 2; ticket07 (renderer service)
/// bumped it to 3; ticket08 (scene kernel service) bumped it to 4;
/// ticket09 (text geometry service) bumped it to 5; ticket10 (glyph
/// raster + atlas services, scene sprite primitives) bumped it to 6;
/// ticket16 (path primitives + the pinned PathBuilder tessellation in
/// the scene service, the scene drawing pipeline in the renderer
/// service) bumped it to 7.
pub const GPUI_GO_NATIVE_REVISION: u32 = 8;

/// gpui-CE source pin this artifact family is built against (ASCII hex,
/// zero-padded to 40 bytes in the table).
pub const GPUI_GO_CE_COMMIT: &str = "254b5dbd47cbb5acbcc5bbdcbb322a339276c88a";

/// Maximum accepted buffer length for the bootstrap round trip, in bytes.
pub const GPUI_GO_MAX_BUFFER_LEN: u32 = 4096;

/// Capability bit assignments for `GpuiGoAbiTable::capabilities` (u64 mask).
///
/// Bits are allocated as service tables land; unassigned bits must remain 0
/// and loaders must reject a missing *required* bit, never an unknown extra
/// bit (additive capabilities are forward-compatible, per contract).
pub mod capabilities {
    /// Bit 0: bootstrap buffer round trip (ticket02).
    pub const BOOTSTRAP_BUFFER_ROUNDTRIP: u64 = 1 << 0;
    /// Bit 1: layout service, Taffy 0.13.0 with the pinned gpui-CE adapter
    /// semantics (ticket06).
    pub const LAYOUT_TAFFY_0_13_0: u64 = 1 << 1;
    /// Bit 2: renderer service, D3D11 clear-frame present with
    /// `D3D11_QUERY_EVENT` retirement (ticket07), following the pinned
    /// gpui-CE Windows renderer's device/swap-chain creation semantics.
    pub const RENDERER_D3D11: u64 = 1 << 2;
    /// Bit 3: scene kernel service, the pinned gpui-CE scene kernel
    /// (bounds-tree draw orders, layers, order floors, finish/sort,
    /// batching, filter planning, paired surface opacity, replay)
    /// (ticket08).
    pub const SCENE_KERNEL: u64 = 1 << 3;
    /// Bit 4: text geometry service, the pinned Parley/Fontique text
    /// stack (system font enumeration, font resolution, shaping, visual
    /// lines, paint fragments, positioned glyphs, carets, selection
    /// rects, hit tests, grapheme clusters) (ticket09).
    pub const TEXT_PARLEY_0_11_1: u64 = 1 << 4;
    /// Bit 5: glyph raster service, the pinned Windows DirectWrite
    /// rasterizer port (AlphaMask/BgraSubpixelMask/BgraColor outputs,
    /// the typed color-font fallback routing) over the shared text
    /// stack (ticket10).
    pub const GLYPH_RASTER_DIRECTWRITE: u64 = 1 << 5;
    /// Bit 6: texture atlas service, the pinned D3D11 sprite atlas port
    /// (etagere bucketed packing, monochrome/subpixel/polychrome pools,
    /// generation-invalidated tiles) on the renderer's shared device
    /// (ticket10).
    pub const ATLAS_D3D11: u64 = 1 << 6;
    /// Bit 7: scene drawing — path primitives and the pinned
    /// PathBuilder tessellation in the scene kernel service (v3), plus
    /// the real scene drawing pipeline of the renderer service (v2:
    /// quads, shadows, underlines, paths with the MSAA intermediate,
    /// sprites, the blur/offscreen filter chain) built from the pinned
    /// `gpui_ce_render` build-time DXBC artifacts (ticket16).
    pub const SCENE_DRAW_PATHS: u64 = 1 << 7;
    /// Bit 8: image codec service, the pinned `image`-crate decode
    /// graph (resource sniffing and clipboard known-format entries,
    /// EXIF orientation, GIF/animated-WebP frame walks with rational
    /// delays, BGRA output) (ticket17).
    pub const IMAGE_CODECS_IMAGE_0_25: u64 = 1 << 8;
    // Reserved for the planned service tables (bits assigned when those
    // tickets land; do not pre-assign):
    //   bit 9+: accessibility/COM service and later
    //   unassigned bits remain 0
}

/// Status codes returned by the bootstrap round trip (and mirrored in the
/// response record's `status` field whenever a response can be written).
pub mod status {
    /// Success; `echoed_len`, `checksum` and the echo bytes are valid.
    pub const OK: i32 = 0;
    /// Request magic did not match [`GPUI_GO_ABI_MAGIC`].
    pub const BAD_MAGIC: i32 = 1;
    /// Request `abi_version` did not match [`GPUI_GO_ABI_VERSION`].
    pub const BAD_ABI_VERSION: i32 = 2;
    /// Request record pointer was null.
    pub const NULL_REQUEST: i32 = 3;
    /// Request length exceeded [`GPUI_GO_MAX_BUFFER_LEN`].
    pub const LENGTH_OVER_MAX: i32 = 4;
    /// Request `data` was null while `len` was non-zero.
    pub const NULL_DATA: i32 = 5;
    /// Response record was null, or the response data buffer was null while
    /// bytes had to be written into it (the caller-owned echo buffer is part
    /// of "the response" for this check).
    pub const NULL_RESPONSE: i32 = 6;
    /// A panic was contained by `catch_unwind` before returning.
    pub const PANIC_CONTAINED: i32 = 7;
}

/// Bootstrap buffer request record (caller-owned, borrowed for the call).
///
/// Layout (x86-64, `#[repr(C)]`): `magic` @0, `abi_version` @4, `len` @8,
/// 4 bytes padding, `data` @16; size 24, alignment 8.
#[repr(C)]
pub struct GpuiGoBufferRequest {
    /// Must equal [`GPUI_GO_ABI_MAGIC`].
    pub magic: u32,
    /// Must equal [`GPUI_GO_ABI_VERSION`].
    pub abi_version: u32,
    /// Requested byte count; `0 <= len <= GPUI_GO_MAX_BUFFER_LEN`.
    pub len: u32,
    /// Pointer to `len` borrowed bytes. May be null only when `len == 0`.
    pub data: *const u8,
}

/// Bootstrap buffer response record (caller-owned, borrowed for the call).
///
/// Layout (x86-64, `#[repr(C)]`): `status` @0, `echoed_len` @4, 4 bytes
/// padding, `checksum` @8, `data` @16; size 24, alignment 8.
#[repr(C)]
pub struct GpuiGoBufferResponse {
    /// Status code; see [`status`]. Always written by the callee, except when
    /// the response record pointer itself is null.
    pub status: i32,
    /// Number of bytes written to `data` (== request `len` on success, else 0).
    pub echoed_len: u32,
    /// FNV-1a 64 checksum of the request bytes (offset basis for empty input).
    pub checksum: u64,
    /// Caller-provided echo buffer, capacity >= request `len` bytes.
    pub data: *mut u8,
}

/// Static bootstrap/service table returned by [`gpui_go_abi`].
///
///
/// Field order places the pointer fields first so the record has one
/// predictable, self-checking layout: all pointer slots sit at 8-byte aligned
/// offsets, followed by the fixed-width scalars. Offsets (x86-64):
///
/// | Offset | Size | Field |
/// |--------|------|-------|
/// | 0 | 8 | `buffer_round_trip` |
/// | 8 | 64 | `reserved[0..8]` (service-table slots) |
/// | 72 | 4 | `magic` |
/// | 76 | 4 | `abi_version` |
/// | 80 | 4 | `native_revision` |
/// | 84 | 40 | `ce_commit` (ASCII hex, zero padded) |
/// | 128 | 8 | `capabilities` (bitmask; 4 bytes padding before it) |
/// | 136 | 4 | `size_of_table` |
/// | 140 | 4 | `align_of_table` |
/// | 144 | 4 | `size_of_buffer_request` |
/// | 148 | 4 | `size_of_buffer_response` |
///
/// Total size 152, alignment 8. The `size_of_*`/`align_of_*` fields let the
/// Go loader verify its mirror struct before reading any other field.
/// A word-sized slot holding the address of a service's static table, or
/// null when the slot is unassigned. The pointee is a read-only static for
/// the process lifetime, so sharing the pointer across threads is sound;
/// `unsafe impl Sync` makes the containing static legal.
/// `#[repr(transparent)]` keeps the slot exactly one pointer wide — the Go
/// loader reads it as uintptr (0 = unassigned).
#[repr(transparent)]
pub struct ServiceTablePtr(pub *const core::ffi::c_void);

// SAFETY: the pointer targets a read-only static table with a stable
// address for the process lifetime (the DLL never frees or mutates it);
// null is a valid, inert value.
unsafe impl Sync for ServiceTablePtr {}

#[repr(C)]
pub struct GpuiGoAbiTable {
    /// Bootstrap buffer round trip entry point. Null is invalid; the loader
    /// must reject a null required slot.
    pub buffer_round_trip:
        Option<unsafe extern "system" fn(*const GpuiGoBufferRequest, *mut GpuiGoBufferResponse) -> i32>,
    /// Reserved service-table slots. Slot k holds a pointer to that
    /// service's static table, or null when unassigned. Word-sized pointer
    /// values; the Go loader reads the slots as uintptr. Slot 1 carries the
    /// layout service table (ticket06); slot 2 carries the renderer service
    /// table (ticket07); slot 3 carries the scene kernel service table
    /// (ticket08); slot 4 carries the text geometry service table
    /// (ticket09); slot 5 carries the glyph raster service table
    /// (ticket10); slot 6 carries the atlas service table (ticket10);
    /// slot 0 and slot 7 remain null.
    pub reserved: [ServiceTablePtr; 8],
    /// [`GPUI_GO_ABI_MAGIC`].
    pub magic: u32,
    /// [`GPUI_GO_ABI_VERSION`].
    pub abi_version: u32,
    /// [`GPUI_GO_NATIVE_REVISION`].
    pub native_revision: u32,
    /// [`GPUI_GO_CE_COMMIT`] as ASCII hex, zero padded to 40 bytes.
    pub ce_commit: [u8; 40],
    /// Capability bitmask; see [`capabilities`].
    pub capabilities: u64,
    /// `size_of::<GpuiGoAbiTable>()` — record size self-check.
    pub size_of_table: u32,
    /// `align_of::<GpuiGoAbiTable>()` — record alignment self-check.
    pub align_of_table: u32,
    /// `size_of::<GpuiGoBufferRequest>()` — record size self-check.
    pub size_of_buffer_request: u32,
    /// `size_of::<GpuiGoBufferResponse>()` — record size self-check.
    pub size_of_buffer_response: u32,
}

// Compile-time layout pins. The Go loader mirrors these sizes exactly; a
// mismatch anywhere fails the load with an ABI-schema error instead of
// silently misreading a field.
const _: () = assert!(size_of::<GpuiGoAbiTable>() == 152);
const _: () = assert!(align_of::<GpuiGoAbiTable>() == 8);
const _: () = assert!(size_of::<GpuiGoBufferRequest>() == 24);
const _: () = assert!(size_of::<GpuiGoBufferResponse>() == 24);
const _: () = assert!(GPUI_GO_CE_COMMIT.len() == 40);

/// FNV-1a 64-bit offset basis and prime.
const FNV1A_OFFSET_BASIS: u64 = 0xcbf2_9ce4_8422_2325;
const FNV1A_PRIME: u64 = 0x0000_0100_0000_01b3;

/// FNV-1a 64 checksum of `bytes` (of the *request* bytes, before any echo).
fn fnv1a64(bytes: &[u8]) -> u64 {
    let mut hash = FNV1A_OFFSET_BASIS;
    for &byte in bytes {
        hash ^= u64::from(byte);
        hash = hash.wrapping_mul(FNV1A_PRIME);
    }
    hash
}

/// `GPUI_GO_CE_COMMIT` as a zero-padded 40-byte ASCII array.
const fn ce_commit_bytes() -> [u8; 40] {
    let mut out = [0u8; 40];
    let src = GPUI_GO_CE_COMMIT.as_bytes();
    let mut i = 0;
    while i < src.len() && i < 40 {
        out[i] = src[i];
        i += 1;
    }
    out
}

/// The one static bootstrap table. Its address is stable for the process
/// lifetime; the DLL never frees or mutates it.
static ABI_TABLE: GpuiGoAbiTable = GpuiGoAbiTable {
    buffer_round_trip: Some(buffer_round_trip),
    reserved: [
        ServiceTablePtr(core::ptr::null()),
        ServiceTablePtr(
            &layout::LAYOUT_TABLE as *const layout::GpuiGoLayoutTable as *const core::ffi::c_void,
        ),
        ServiceTablePtr(
            &renderer::RENDERER_TABLE as *const renderer::GpuiGoRendererTable as *const core::ffi::c_void,
        ),
        ServiceTablePtr(
            &scene::SCENE_TABLE as *const scene::GpuiGoSceneTable as *const core::ffi::c_void,
        ),
        ServiceTablePtr(
            &text::TEXT_TABLE as *const text::GpuiGoTextTable as *const core::ffi::c_void,
        ),
        ServiceTablePtr(
            &glyph::GLYPH_TABLE as *const glyph::GpuiGoGlyphTable as *const core::ffi::c_void,
        ),
        ServiceTablePtr(
            &atlas::ATLAS_TABLE as *const atlas::GpuiGoAtlasTable as *const core::ffi::c_void,
        ),
        ServiceTablePtr(
            &image::IMAGE_TABLE as *const image::GpuiGoImageTable as *const core::ffi::c_void,
        ),
    ],
    magic: GPUI_GO_ABI_MAGIC,
    abi_version: GPUI_GO_ABI_VERSION,
    native_revision: GPUI_GO_NATIVE_REVISION,
    ce_commit: ce_commit_bytes(),
    capabilities: capabilities::BOOTSTRAP_BUFFER_ROUNDTRIP
        | capabilities::LAYOUT_TAFFY_0_13_0
        | capabilities::RENDERER_D3D11
        | capabilities::SCENE_KERNEL
        | capabilities::IMAGE_CODECS_IMAGE_0_25
        | capabilities::TEXT_PARLEY_0_11_1
        | capabilities::GLYPH_RASTER_DIRECTWRITE
        | capabilities::ATLAS_D3D11
        | capabilities::SCENE_DRAW_PATHS,
    size_of_table: size_of::<GpuiGoAbiTable>() as u32,
    align_of_table: align_of::<GpuiGoAbiTable>() as u32,
    size_of_buffer_request: size_of::<GpuiGoBufferRequest>() as u32,
    size_of_buffer_response: size_of::<GpuiGoBufferResponse>() as u32,
};

/// Returns a pointer to the static [`GpuiGoAbiTable`].
///
/// The table is read-only; the caller must not write through the pointer and
/// may rely on it for the process lifetime (no hot unload by design).
#[unsafe(no_mangle)]
pub extern "system" fn gpui_go_abi() -> *const GpuiGoAbiTable {
    &ABI_TABLE
}

/// Test-only panic containment probe: deliberately panics inside
/// `catch_unwind` and returns [`status::PANIC_CONTAINED`].
///
/// This export exists so the Go conformance tests can observe a *real*
/// contained panic (the round-trip body itself contains no operation that can
/// panic). It performs no other work and must never be called by application
/// code.
#[unsafe(no_mangle)]
pub extern "system" fn gpui_go_panic_probe() -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| {
        panic!("gpui_go panic probe (contained by design)");
    }));
    match outcome {
        // Unreachable: the closure always panics. Returning OK here would be
        // a probe bug, not a loader concern.
        Ok(()) => status::OK,
        Err(payload) => {
            // Drop the payload here; nothing is retained across the call.
            drop(payload);
            status::PANIC_CONTAINED
        }
    }
}

/// Validates the request, computes the checksum and copies the echo bytes
/// into the caller-provided response buffer.
///
/// Everything is borrowed for the duration of the call: no allocation, no
/// retained pointers, no threads. The order of validation is part of the
/// contract: null records first, then magic, then ABI version, then length
/// bound, then data nullability.
///
/// # Safety
///
/// `request` and `response`, when non-null, must point to valid, exclusively
/// borrowed records for the duration of the call, and `response.data` (when
/// `request.len > 0`) must point to a writable buffer of at least
/// `request.len` bytes.
unsafe fn round_trip_body(
    request: *const GpuiGoBufferRequest,
    response: *mut GpuiGoBufferResponse,
) -> i32 {
    unsafe {
        if request.is_null() {
            return status::NULL_REQUEST;
        }
        if response.is_null() {
            return status::NULL_RESPONSE;
        }
        let req = &*request;
        let res = &mut *response;

        // Start from a clean response so error paths never leak stale values.
        res.status = status::OK;
        res.echoed_len = 0;
        res.checksum = 0;

        if req.magic != GPUI_GO_ABI_MAGIC {
            res.status = status::BAD_MAGIC;
            return status::BAD_MAGIC;
        }
        if req.abi_version != GPUI_GO_ABI_VERSION {
            res.status = status::BAD_ABI_VERSION;
            return status::BAD_ABI_VERSION;
        }
        if req.len > GPUI_GO_MAX_BUFFER_LEN {
            res.status = status::LENGTH_OVER_MAX;
            return status::LENGTH_OVER_MAX;
        }
        if req.len > 0 && req.data.is_null() {
            res.status = status::NULL_DATA;
            return status::NULL_DATA;
        }
        if req.len > 0 && res.data.is_null() {
            // Documented extension of status 6: the response is unusable for
            // writing the echo the caller asked for.
            res.status = status::NULL_RESPONSE;
            return status::NULL_RESPONSE;
        }

        // Build the borrowed slice without touching a null data pointer.
        let bytes: &[u8] = if req.len == 0 {
            &[]
        } else {
            // Safe here: non-null and len <= 4096 was validated above.
            std::slice::from_raw_parts(req.data, req.len as usize)
        };

        res.checksum = fnv1a64(bytes);
        res.echoed_len = req.len;
        if req.len > 0 {
            std::ptr::copy_nonoverlapping(req.data, res.data, req.len as usize);
        }
        res.status = status::OK;
        status::OK
    }
}

/// Table entry for the bootstrap buffer round trip. The panic boundary: every
/// panic is contained here and becomes status 7; the unwind never crosses the
/// ABI into the Go caller.
unsafe extern "system" fn buffer_round_trip(
    request: *const GpuiGoBufferRequest,
    response: *mut GpuiGoBufferResponse,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| unsafe {
        round_trip_body(request, response)
    }));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            // Drop the payload; nothing is retained.
            drop(payload);
            if !response.is_null() {
                unsafe {
                    (*response).status = status::PANIC_CONTAINED;
                    (*response).echoed_len = 0;
                    (*response).checksum = 0;
                }
            }
            status::PANIC_CONTAINED
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn fnv1a64_known_vectors() {
        assert_eq!(fnv1a64(b""), 0xcbf29ce484222325);
        assert_eq!(fnv1a64(b"a"), 0xaf63dc4c8601ec8c);
        assert_eq!(fnv1a64(b"foobar"), 0x85944171f73967e8);
    }

    #[test]
    fn table_identity_constants() {
        assert_eq!(ABI_TABLE.magic, GPUI_GO_ABI_MAGIC);
        assert_eq!(ABI_TABLE.abi_version, 1);
        assert_eq!(ABI_TABLE.native_revision, GPUI_GO_NATIVE_REVISION);
        assert_eq!(&ABI_TABLE.ce_commit, GPUI_GO_CE_COMMIT.as_bytes());
        assert_eq!(
            ABI_TABLE.capabilities,
            capabilities::BOOTSTRAP_BUFFER_ROUNDTRIP
                | capabilities::LAYOUT_TAFFY_0_13_0
                | capabilities::RENDERER_D3D11
                | capabilities::SCENE_KERNEL
                | capabilities::TEXT_PARLEY_0_11_1
                | capabilities::GLYPH_RASTER_DIRECTWRITE
                | capabilities::ATLAS_D3D11
                | capabilities::SCENE_DRAW_PATHS
        );
        assert!(ABI_TABLE.buffer_round_trip.is_some());
        assert!(
            ABI_TABLE.reserved[0].0.is_null(),
            "slot 0 stays unassigned"
        );
        assert!(
            !ABI_TABLE.reserved[1].0.is_null(),
            "slot 1 carries the layout table"
        );
        assert!(
            !ABI_TABLE.reserved[2].0.is_null(),
            "slot 2 carries the renderer table"
        );
        assert!(
            !ABI_TABLE.reserved[3].0.is_null(),
            "slot 3 carries the scene table"
        );
        assert!(
            !ABI_TABLE.reserved[4].0.is_null(),
            "slot 4 carries the text table"
        );
        assert!(
            !ABI_TABLE.reserved[5].0.is_null(),
            "slot 5 carries the glyph table"
        );
        assert!(
            !ABI_TABLE.reserved[6].0.is_null(),
            "slot 6 carries the atlas table"
        );
        assert!(ABI_TABLE.reserved[0].0.is_null());
        assert!(ABI_TABLE.reserved[7].0.is_null());
    }

    #[test]
    fn table_self_check_fields_match_memory_truth() {
        assert_eq!(ABI_TABLE.size_of_table as usize, size_of::<GpuiGoAbiTable>());
        assert_eq!(ABI_TABLE.align_of_table as usize, align_of::<GpuiGoAbiTable>());
        assert_eq!(
            ABI_TABLE.size_of_buffer_request as usize,
            size_of::<GpuiGoBufferRequest>()
        );
        assert_eq!(
            ABI_TABLE.size_of_buffer_response as usize,
            size_of::<GpuiGoBufferResponse>()
        );
    }

    #[test]
    fn round_trip_happy_path() {
        let payload: &[u8] = b"gpui-go bootstrap";
        let mut echo = [0u8; 17];
        let req = GpuiGoBufferRequest {
            magic: GPUI_GO_ABI_MAGIC,
            abi_version: GPUI_GO_ABI_VERSION,
            len: payload.len() as u32,
            data: payload.as_ptr(),
        };
        let mut res = GpuiGoBufferResponse {
            status: -1,
            echoed_len: u32::MAX,
            checksum: 0,
            data: echo.as_mut_ptr(),
        };
        let code = unsafe {
            buffer_round_trip(&req, &mut res)
        };
        assert_eq!(code, status::OK);
        assert_eq!(res.status, status::OK);
        assert_eq!(res.echoed_len, payload.len() as u32);
        assert_eq!(res.checksum, fnv1a64(payload));
        assert_eq!(&echo, payload);
    }

    #[test]
    fn round_trip_status_codes() {
        let mut res = GpuiGoBufferResponse {
            status: -1,
            echoed_len: u32::MAX,
            checksum: 0,
            data: core::ptr::null_mut(),
        };
        // Null request.
        assert_eq!(unsafe { buffer_round_trip(core::ptr::null(), &mut res) }, status::NULL_REQUEST);
        // Null response.
        let ok_req = GpuiGoBufferRequest { magic: GPUI_GO_ABI_MAGIC, abi_version: GPUI_GO_ABI_VERSION, len: 0, data: core::ptr::null() };
        assert_eq!(unsafe { buffer_round_trip(&ok_req, core::ptr::null_mut()) }, status::NULL_RESPONSE);
        // Bad magic.
        let req = GpuiGoBufferRequest { magic: 0xDEAD_BEEF, ..ok_req };
        assert_eq!(unsafe { buffer_round_trip(&req, &mut res) }, status::BAD_MAGIC);
        // Bad ABI version.
        let req = GpuiGoBufferRequest { abi_version: 2, ..ok_req };
        assert_eq!(unsafe { buffer_round_trip(&req, &mut res) }, status::BAD_ABI_VERSION);
        // Length over max.
        let req = GpuiGoBufferRequest { len: GPUI_GO_MAX_BUFFER_LEN + 1, ..ok_req };
        assert_eq!(unsafe { buffer_round_trip(&req, &mut res) }, status::LENGTH_OVER_MAX);
        // Null data with non-zero len.
        let req = GpuiGoBufferRequest { len: 4, ..ok_req };
        assert_eq!(unsafe { buffer_round_trip(&req, &mut res) }, status::NULL_DATA);
    }

    #[test]
    fn panic_probe_reports_containment() {
        assert_eq!(gpui_go_panic_probe(), status::PANIC_CONTAINED);
    }
}
