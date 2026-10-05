//! gpui-go native renderer service (ticket07).
//!
//! Implements the renderer ABI for the "present a clear frame and retire
//! its resources on GPU completion" slice: a per-process D3D11 device
//! created exactly like the pinned gpui-CE Windows renderer, one swap chain
//! per surface in either of the pinned composition modes, a clear+present
//! submission path with a `D3D11_QUERY_EVENT` completion marker, and an
//! acceptance-vs-retirement protocol (`submission_poll` / `submission_retire`).
//!
//! The service table is installed in **reserved slot 2** of the bootstrap
//! `GpuiGoAbiTable` and advertised with capability bit 2
//! (`renderer-d3d11`).
//!
//! # Pinned source semantics (all citations `254b5dbd…`, the CE pin)
//!
//! * **Device creation** — `crates/gpui_windows/src/directx_devices.rs`:
//!   `IDXGIFactory6` via `CreateDXGIFactory2` (debug flag only when the
//!   debug layer is available, which release builds never probe), adapter
//!   enumeration with `EnumAdapters` until `DXGI_ERROR_NOT_FOUND`,
//!   `D3D11CreateDevice` per adapter with `D3D_DRIVER_TYPE_UNKNOWN`,
//!   `D3D11_CREATE_DEVICE_BGRA_SUPPORT` (+ `D3D11_CREATE_DEVICE_DEBUG`
//!   with the debug layer), feature levels `[11_1, 11_0]` (SM5-only
//!   rationale from the pin), `D3D11_SDK_VERSION`; the first adapter whose
//!   device creation succeeds wins and is recorded. No WARP fallback is
//!   invented: no compatible adapter is a typed error.
//! * **Swap chains** — `directx_renderer.rs::create_swap_chain_for_composition`
//!   (DComp mode) and `::create_swap_chain` (HWND mode): format
//!   `DXGI_FORMAT_B8G8R8A8_UNORM`, sample count 1/quality 0, usage
//!   `DXGI_USAGE_RENDER_TARGET_OUTPUT`, **buffer count 3** (the pin's
//!   `BUFFER_COUNT`, flip-sequential triple buffering), swap effect
//!   `DXGI_SWAP_EFFECT_FLIP_SEQUENTIAL`, scaling `DXGI_SCALING_STRETCH`
//!   for DComp vs `DXGI_SCALING_NONE` for HWND, alpha mode
//!   `DXGI_ALPHA_MODE_PREMULTIPLIED` for DComp vs
//!   `DXGI_ALPHA_MODE_IGNORE` for HWND, flags 0. HWND mode also calls
//!   `MakeWindowAssociation(hwnd, DXGI_MWA_NO_ALT_ENTER)`.
//! * **DirectComposition** — `directx_renderer.rs::DirectComposition`:
//!   `DCompositionCreateDevice(dxgi_device)` (the DXGI device comes from
//!   casting the D3D11 device), `CreateTargetForHwnd(hwnd, true)`,
//!   `CreateVisual`, `SetContent(swap_chain)`, `SetRoot(visual)`,
//!   `Commit`.
//! * **Clear + present** — `directx_renderer.rs::pre_draw`/`present`:
//!   `ClearRenderTargetView` with the caller color, `Present(0,
//!   DXGI_PRESENT(0))`, render-target/viewport binding in the pin's order.
//! * **Resize** — `directx_renderer.rs::resize`: unbind render targets,
//!   drop the back-buffer views, `ResizeBuffers(3, w, h, format, 0)`,
//!   recreate the views and viewport, rebind; unchanged extents return
//!   early; extents clamp to >= 1.
//! * **Device loss detection** — `platform.rs::check_device_lost`:
//!   `GetDeviceRemovedReason()`; `Ok` = alive, `Err(code)` = removed.
//! * **Driver identity** — `directx_renderer.rs::gpu_specs` + the `nvidia`
//!   /`amd`/`dxgi` modules: vendor-id to driver-name mapping
//!   (0x10DE NVIDIA, 0x1002 AMD, 0x8086 Intel, else unknown-vendor), the
//!   driver version via NvAPI (`nvapi_QueryInterface(0x2926aaad)`), AMD
//!   AGS, or DXGI `CheckInterfaceSupport(&IDXGIDevice::IID)`, with an
//!   "Unknown Driver" fallback.
//!
//! # Deviations from the pin (each required by the renderer contract or
//! the ABI boundary, none of them silent)
//!
//! * The pin has **no per-submission completion protocol**
//!   (`docs/renderer-contract.md`: "Add one at the service seam; do not
//!   call it preserved source behavior"). This service adds
//!   `D3D11_QUERY_EVENT` + `GetData` retirement.
//! * The pin calls `Flush()` only in its device-lost recovery path. The
//!   contract requires the marker to be *actually submitted* even when
//!   rendering goes idle or a window is hidden, and one submission here is
//!   exactly the idle case (there is no later frame that would carry the
//!   marker), so every submission ends the query and then flushes once.
//!   `Present` remains a submission/presentation result, never completion
//!   evidence; `Flush` is asynchronous and never an acknowledgment.
//! * The pin logs adapter names through the `log` crate; this service
//!   records them in the device-info records instead (the native artifact
//!   carries no logging dependency).
//! * Multithread protection (`ID3D11Multithread::SetMultithreadProtected`)
//!   is set on the shared device's immediate context, as the renderer
//!   contract requires for the shared device; the pin (single-window
//!   rendering from one thread) does not set it. The previous value the
//!   API reports is recorded in the device-info record, per the contract's
//!   note that the return value reports the previous setting.
//!
//! # ABI rules (same conventions as `LAYOUT_ABI.md`)
//!
//! * C ABI, `extern "system"`, `#[repr(C)]`, fixed-width types; no Rust
//!   strings/Vecs/enums/bools cross the boundary; no Go pointers are
//!   retained by native code.
//! * Every export contains panics with `catch_unwind` and returns a
//!   status; a contained panic returns [`renderer_status::ERR_PANIC`]
//!   without poisoning the service (the state mutex is released during
//!   unwind and recovered).
//! * Handles are validated opaque u64s. Surface handles encode
//!   `(slot index << 20) | slot generation`; submission handles encode
//!   `(slot index << 52) | (slot generation << 32) | submission sequence`.
//!   Stale (past generation / retired) use returns
//!   [`renderer_status::ERR_STALE_HANDLE`]; malformed or never-issued
//!   handles return [`renderer_status::ERR_BAD_HANDLE`].
//! * Immediate-context serialization: one service-global mutex guards the
//!   device, every surface and every submission record; every export takes
//!   it. No lock is held across any callback — the service has no Go
//!   callbacks; completion is polled, never trampolined.
//! * Pending submissions are bounded (64 per surface); exceeding the cap
//!   fails with the typed busy error
//!   [`renderer_status::ERR_BUSY`] before any GPU work is accepted.
//! * Quarantine semantics: a submission that failed **without confirmed
//!   device loss** stays tracked — retirement is refused with
//!   [`renderer_status::ERR_QUARANTINE`] until a retire-time
//!   `GetDeviceRemovedReason` confirms loss. Retirement on confirmed loss
//!   is the documented release path (terminal "aborted").
//! * `surface_destroy` refuses while un-retired submissions remain
//!   ([`renderer_status::ERR_PENDING`]); the Go ledger drains them first.

use std::collections::HashMap;
use std::panic::{catch_unwind, AssertUnwindSafe};
use std::slice;
use std::sync::Mutex;

mod draw;

use draw::{MAX_INSTANCE_BUFFER_SIZE, PATH_MULTISAMPLE_COUNT};

use windows::core::Interface;
use windows::Win32::Foundation::{FreeLibrary, HMODULE, HWND};
use windows::Win32::Graphics::Direct3D::{
    D3D_DRIVER_TYPE_UNKNOWN, D3D_FEATURE_LEVEL, D3D_FEATURE_LEVEL_11_0, D3D_FEATURE_LEVEL_11_1,
};
use windows::Win32::Graphics::Direct3D11::{
    ID3D11Device, ID3D11DeviceContext, ID3D11Multithread, ID3D11Query, ID3D11RenderTargetView,
    ID3D11Texture2D, D3D11_CREATE_DEVICE_BGRA_SUPPORT, D3D11_CREATE_DEVICE_DEBUG, D3D11_QUERY_DESC,
    D3D11_QUERY_EVENT, D3D11_SDK_VERSION, D3D11_VIEWPORT, D3D11CreateDevice,
};
use windows::Win32::Graphics::DirectComposition::{
    DCompositionCreateDevice, IDCompositionDevice, IDCompositionTarget, IDCompositionVisual,
};
use windows::Win32::Graphics::Dxgi::Common::{
    DXGI_ALPHA_MODE_IGNORE, DXGI_ALPHA_MODE_PREMULTIPLIED, DXGI_FORMAT_B8G8R8A8_UNORM,
    DXGI_SAMPLE_DESC,
};
use windows::Win32::Graphics::Dxgi::{
    CreateDXGIFactory2, DXGI_ADAPTER_FLAG_SOFTWARE, DXGI_CREATE_FACTORY_DEBUG,
    DXGI_CREATE_FACTORY_FLAGS, DXGI_ERROR_NOT_FOUND, DXGI_MWA_NO_ALT_ENTER, DXGI_PRESENT,
    DXGI_SCALING_NONE, DXGI_SCALING_STRETCH, DXGI_SWAP_CHAIN_DESC1, DXGI_SWAP_CHAIN_FLAG,
    DXGI_SWAP_EFFECT_FLIP_SEQUENTIAL, DXGI_USAGE_RENDER_TARGET_OUTPUT, IDXGIAdapter1, IDXGIDevice,
    IDXGIFactory6, IDXGISwapChain1,
};
use windows::Win32::System::LibraryLoader::LoadLibraryA;

/// Renderer service ABI version (bumped only on record/table layout
/// changes of the renderer service). Version 2 (ticket16) added the
/// scene drawing entries (`surface_draw_scene` / `surface_render_scene`
/// / `surface_read_pixels`) and the draw-constant scalars
/// (`max_instance_buffer_bytes`, `path_multisample_count`).
pub const GPUI_GO_RENDERER_SERVICE_VERSION: u32 = 2;

/// The pin's swap-chain buffer count (`BUFFER_COUNT` in
/// `directx_renderer.rs`): flip-sequential triple buffering.
pub const RENDERER_BUFFER_COUNT: u32 = 3;

/// Render-target format the pin uses (`RENDER_TARGET_FORMAT`): 8-bit BGRA
/// UNORM, DXGI format value 87.
pub const RENDER_TARGET_FORMAT_VALUE: u32 = 87;

/// Maximum tracked (un-retired) submissions per surface. Exceeding it
/// fails `surface_present` with the typed busy error before any GPU work
/// is accepted — the bounded pending-submission rule of the renderer
/// contract.
pub const MAX_PENDING_SUBMISSIONS: u32 = 64;

/// D3D11 feature levels accepted by the pin (SM5-only rationale in
/// `directx_devices.rs::get_device`).
const FEATURE_LEVELS: [D3D_FEATURE_LEVEL; 2] = [D3D_FEATURE_LEVEL_11_1, D3D_FEATURE_LEVEL_11_0];

/// Renderer service status codes. 0 is success; negative values are
/// caller/argument errors — with one documented exception:
/// [`ERR_PRESENT`] is a tracked partial-submit outcome, not a caller
/// mistake — and 100+ are internal failures.
pub mod renderer_status {
    /// Success.
    pub const OK: i32 = 0;
    /// The handle encoded a past generation (surface destroyed, submission
    /// retired, or slot reused since).
    pub const ERR_STALE_HANDLE: i32 = -1;
    /// The handle never existed or is malformed (out-of-range slot, a
    /// never-issued submission sequence).
    pub const ERR_BAD_HANDLE: i32 = -2;
    /// A required pointer argument was null.
    pub const ERR_NULL_ARG: i32 = -3;
    /// An invalid mode, extent or record-size value.
    pub const ERR_BAD_VALUE: i32 = -4;
    /// The shared D3D11 device could not be created (no compatible
    /// adapter, per the pin's enumeration semantics — no WARP fallback).
    pub const ERR_DEVICE: i32 = -5;
    /// The per-surface pending-submission cap was exceeded; backpressure
    /// before any GPU work was accepted.
    pub const ERR_BUSY: i32 = -6;
    /// The operation was refused because un-retired submissions remain
    /// (retire of a pending submission, surface destroy with outstanding
    /// submissions).
    pub const ERR_PENDING: i32 = -7;
    /// Retirement was refused: the submission failed without confirmed
    /// device loss, so its record stays tracked (quarantine) until loss is
    /// confirmed.
    pub const ERR_QUARANTINE: i32 = -8;
    /// The Present call failed. NOT a caller error: the submission handle
    /// in `out_submission` is valid and tracked (the partial-submit rule
    /// of the renderer contract) and must be polled and retired.
    pub const ERR_PRESENT: i32 = -9;
    /// The device-info string buffer was too small; `out_needed` carries
    /// the required byte count.
    pub const ERR_CAPACITY: i32 = -10;
    /// The pinned surface import error (ticket16's draw path): the
    /// scene's surface source is the `SurfaceSource::Unsupported`
    /// stand-in and the pinned draw path bails on it ("DirectX renderer
    /// cannot import this surface source"). Explicit, not a blank
    /// successful draw; pixel output for imported surface sources awaits
    /// the capture-producer slice.
    pub const ERR_UNSUPPORTED_SOURCE: i32 = -11;
    /// The scene handle of a draw entry did not resolve to a finished,
    /// healthy scene (stale, malformed, unfinished or failed).
    pub const ERR_SCENE: i32 = -12;
    /// A panic was contained by `catch_unwind` during this call.
    pub const ERR_PANIC: i32 = 101;
}

/// Submission state values (`GpuiGoSubmissionStateRecord.state`).
pub mod submission_state {
    /// `GetData` returned `S_FALSE`: the GPU has not finished; the record
    /// stays pending.
    pub const PENDING: u32 = 0;
    /// `GetData` returned `S_OK` (data available): the submission's GPU
    /// work completed; retirement is allowed.
    pub const COMPLETED: u32 = 1;
    /// The query errored (including device removal): the failure is
    /// recorded; retirement only on confirmed device loss.
    pub const FAILED: u32 = 2;
}

/// Terminal record values (`GpuiGoRetireRecord.terminal`).
pub mod retire_terminal {
    /// The submission retired after an observed completed poll.
    pub const COMPLETED: u32 = 0;
    /// The submission retired as an abort after a retire-time
    /// `GetDeviceRemovedReason` confirmed device loss (the documented
    /// release path).
    pub const ABORTED_LOSS: u32 = 1;
}

/// Surface composition modes (`surface_create`'s mode argument).
pub mod surface_mode {
    /// DirectComposition swap chain, premultiplied alpha (the pin's
    /// default composition path).
    pub const DCOMP_PREMULTIPLIED: u32 = 0;
    /// Plain `CreateSwapChainForHwnd` swap chain, alpha ignored (the pin's
    /// `GPUI_DISABLE_DIRECT_COMPOSITION` path).
    pub const HWND_ALPHA_IGNORE: u32 = 1;
}

// ---------------------------------------------------------------------------
// Records
// ---------------------------------------------------------------------------

/// Surface facts for `surface_info`. Layout (x86-64, `#[repr(C)]`): `mode`
/// @0, `format` @4, `buffer_count` @8, `width` @12, `height` @16,
/// `feature_level` @20, `resize_generation` @24, `scale_bits` @28,
/// `record_size` @32; size 36, alignment 4.
#[repr(C)]
#[derive(Clone, Copy, PartialEq, Debug)]
pub struct GpuiGoSurfaceInfoRecord {
    /// One of the [`surface_mode`] values the surface was created with.
    pub mode: u32,
    /// The swap-chain buffer format as a raw DXGI_FORMAT value (87 =
    /// `DXGI_FORMAT_B8G8R8A8_UNORM`).
    pub format: u32,
    /// The pin's buffer count (3, flip-sequential).
    pub buffer_count: u32,
    /// Current back-buffer width in device pixels.
    pub width: u32,
    /// Current back-buffer height in device pixels.
    pub height: u32,
    /// The device's actual feature level (raw `D3D_FEATURE_LEVEL` value,
    /// e.g. 0xb100 for 11.1).
    pub feature_level: u32,
    /// Bumped by every swap-chain resize.
    pub resize_generation: u32,
    /// f32 bits of the scale factor recorded at create/resize time.
    pub scale_bits: u32,
    /// `size_of::<GpuiGoSurfaceInfoRecord>()` self-check.
    pub record_size: u32,
}

/// One `submission_poll` result. Layout: `state` @0,
/// `device_removed_reason` @4, `data_hr` @8, `present_hr` @12,
/// `record_size` @16; size 20, alignment 4.
#[repr(C)]
#[derive(Clone, Copy, PartialEq, Debug)]
pub struct GpuiGoSubmissionStateRecord {
    /// One of the [`submission_state`] values.
    pub state: u32,
    /// The device-removed reason HRESULT when the failure was confirmed
    /// through `GetDeviceRemovedReason`; 0 otherwise (pending, completed,
    /// or a query error without confirmed loss).
    pub device_removed_reason: i32,
    /// The raw HRESULT of the last `GetData` poll (0 = S_OK, 1 = S_FALSE,
    /// negative = error). Diagnostics; S_OK/S_FALSE are the only
    /// completion evidence.
    pub data_hr: i32,
    /// The HRESULT the Present call returned when the submission was
    /// accepted (0 = success). A non-zero value marks the partial-submit
    /// path; the submission is still tracked and must be retired.
    pub present_hr: i32,
    /// `size_of::<GpuiGoSubmissionStateRecord>()` self-check.
    pub record_size: u32,
}

/// The exactly-once terminal record produced by `submission_retire`.
/// Layout: `terminal` @0, `device_removed_reason` @4, `reserved` @8,
/// `record_size` @16; size 20, alignment 4.
#[repr(C)]
#[derive(Clone, Copy, PartialEq, Debug)]
pub struct GpuiGoRetireRecord {
    /// One of the [`retire_terminal`] values.
    pub terminal: u32,
    /// The confirmed removal reason for an aborted retire; 0 otherwise.
    pub device_removed_reason: i32,
    /// Reserved; zero.
    pub reserved: [u32; 2],
    /// `size_of::<GpuiGoRetireRecord>()` self-check.
    pub record_size: u32,
}

/// Device/environment identity. The three strings (adapter description,
/// driver name, driver version) are UTF-8, packed contiguously in that
/// order into the caller-provided byte buffer; the offset/length pairs
/// address them.
///
/// Layout: `vendor_id` @0, `device_id` @4, `feature_level` @8,
/// `adapter_flags` @12, `reserved` @16, `dedicated_video_memory` @24,
/// `multithread_protected` @32, `adapter_desc_offset` @36,
/// `adapter_desc_len` @40, `driver_name_offset` @44, `driver_name_len`
/// @48, `driver_version_offset` @52, `driver_version_len` @56,
/// `record_size` @60; size 64, alignment 8.
#[repr(C)]
#[derive(Clone, Copy, PartialEq, Debug)]
pub struct GpuiGoDeviceInfoRecord {
    /// DXGI adapter vendor id (e.g. 0x10DE NVIDIA).
    pub vendor_id: u32,
    /// DXGI adapter device id.
    pub device_id: u32,
    /// The feature level actually selected for the shared device.
    pub feature_level: u32,
    /// Bit 0 set when the adapter is software (DXGI_ADAPTER_FLAG_SOFTWARE).
    pub adapter_flags: u32,
    /// Reserved; must be zero.
    pub reserved: u32,
    /// `DXGI_ADAPTER_DESC1.DedicatedVideoMemory` in bytes.
    pub dedicated_video_memory: u64,
    /// 1 when the immediate context's multithread protection is enabled
    /// (set on device creation, per the renderer contract).
    pub multithread_protected: u32,
    /// Adapter description (UTF-8) offset into the string buffer.
    pub adapter_desc_offset: u32,
    /// Adapter description length in bytes.
    pub adapter_desc_len: u32,
    /// Driver name (UTF-8) offset.
    pub driver_name_offset: u32,
    /// Driver name length.
    pub driver_name_len: u32,
    /// Driver version (UTF-8) offset.
    pub driver_version_offset: u32,
    /// Driver version length.
    pub driver_version_len: u32,
    /// `size_of::<GpuiGoDeviceInfoRecord>()` self-check.
    pub record_size: u32,
}

// Compile-time layout pins; the Go loader mirrors these exactly.
const _: () = assert!(std::mem::size_of::<GpuiGoSurfaceInfoRecord>() == 36);
const _: () = assert!(std::mem::align_of::<GpuiGoSurfaceInfoRecord>() == 4);
const _: () = assert!(std::mem::size_of::<GpuiGoSubmissionStateRecord>() == 20);
const _: () = assert!(std::mem::align_of::<GpuiGoSubmissionStateRecord>() == 4);
const _: () = assert!(std::mem::size_of::<GpuiGoRetireRecord>() == 20);
const _: () = assert!(std::mem::align_of::<GpuiGoRetireRecord>() == 4);
const _: () = assert!(std::mem::size_of::<GpuiGoDeviceInfoRecord>() == 64);
const _: () = assert!(std::mem::align_of::<GpuiGoDeviceInfoRecord>() == 8);
const _: () = assert!(std::mem::size_of::<GpuiGoRendererTable>() == 128);
const _: () = assert!(std::mem::align_of::<GpuiGoRendererTable>() == 8);

// ---------------------------------------------------------------------------
// Handle packing
// ---------------------------------------------------------------------------

/// Surface handle: `(slot index << 20) | slot generation`, slot generation
/// in the low 20 bits (same encoding as the layout engine handles).
const SURFACE_HANDLE_SLOT_SHIFT: u32 = 20;
/// Maximum slot generation (20 bits).
const SURFACE_GENERATION_MAX: u32 = 0x000F_FFFF;

/// Submission handle: `(slot index << 52) | (slot generation << 32) |
/// submission sequence`. 12-bit slot (up to 4096 concurrent surfaces),
/// 20-bit generation, 32-bit per-surface sequence.
const SUBMISSION_SLOT_SHIFT: u32 = 52;
const SUBMISSION_GENERATION_SHIFT: u32 = 32;
/// Maximum submission slot index (12 bits).
const SUBMISSION_SLOT_MAX: u32 = 0xFFF;
/// Mask of the submission-sequence bits.
const SUBMISSION_SEQ_MASK: u64 = 0xFFFF_FFFF;

fn surface_handle_parts(handle: u64) -> (usize, u32) {
    (
        (handle >> SURFACE_HANDLE_SLOT_SHIFT) as usize,
        (handle & SURFACE_GENERATION_MAX as u64) as u32,
    )
}

fn make_surface_handle(slot: usize, generation: u32) -> u64 {
    ((slot as u64) << SURFACE_HANDLE_SLOT_SHIFT) | generation as u64
}

fn make_submission_handle(slot: usize, generation: u32, sequence: u64) -> u64 {
    ((slot as u64) << SUBMISSION_SLOT_SHIFT)
        | ((generation as u64) << SUBMISSION_GENERATION_SHIFT)
        | (sequence & SUBMISSION_SEQ_MASK)
}

fn submission_handle_parts(handle: u64) -> (usize, u32, u64) {
    (
        (handle >> SUBMISSION_SLOT_SHIFT) as usize,
        ((handle >> SUBMISSION_GENERATION_SHIFT) as u32) & SURFACE_GENERATION_MAX,
        handle & SUBMISSION_SEQ_MASK,
    )
}

// ---------------------------------------------------------------------------
// Native state
// ---------------------------------------------------------------------------

/// Environment identity facts captured when the shared device was created.
struct DeviceFacts {
    vendor_id: u32,
    device_id: u32,
    software: bool,
    dedicated_video_memory: u64,
    feature_level: u32,
    adapter_desc: String,
    driver_name: String,
    driver_version: String,
    multithread_protected: bool,
}

/// The per-process shared D3D11 device and its immediate context, created
/// on first use exactly like the pin's `DirectXDevices::new`.
struct DeviceShared {
    dxgi_factory: IDXGIFactory6,
    adapter: IDXGIAdapter1,
    device: ID3D11Device,
    context: ID3D11DeviceContext,
    facts: DeviceFacts,
}

/// The pin's `DirectComposition` tree for mode-0 surfaces.
struct CompositionTree {
    #[expect(dead_code)]
    comp_device: IDCompositionDevice,
    #[expect(dead_code)]
    comp_target: IDCompositionTarget,
    #[expect(dead_code)]
    comp_visual: IDCompositionVisual,
}

/// One tracked submission: the query object plus its state. Acceptance
/// (creation) and retirement (removal) are separate events; polling may
/// observe the state many times in between.
struct SubmissionRecord {
    #[expect(dead_code)]
    sequence: u64,
    query: Option<ID3D11Query>,
    state: SubmissionState,
    /// HRESULT returned by the Present call at acceptance (0 = success).
    present_hr: i32,
    /// Raw HRESULT of the most recent GetData poll (diagnostics).
    data_hr: i32,
}

enum SubmissionState {
    /// Marker submitted; `GetData` has not returned S_OK yet.
    Pending,
    /// `GetData` returned S_OK: the GPU work completed.
    Completed,
    /// The query errored. `reason` is the device-removed reason when loss
    /// was confirmed, otherwise the query error code.
    Failed { reason: i32 },
}

/// One surface: a swap chain in one of the two pinned modes plus its
/// tracked submissions.
struct SurfaceState {
    #[expect(dead_code)]
    hwnd: u64,
    mode: u32,
    width: u32,
    height: u32,
    scale_bits: u32,
    resize_generation: u32,
    swap_chain: IDXGISwapChain1,
    render_target: Option<ID3D11Texture2D>,
    render_target_view: Option<ID3D11RenderTargetView>,
    viewport: D3D11_VIEWPORT,
    /// Ticket16: the surface's lazily created, size-dependent path and
    /// blur intermediate targets (dropped on resize like the pin's
    /// `recreate_resources`).
    draw_targets: draw::DrawTargets,
    /// The DComp visual tree of a mode-0 surface. Retained (never read
    /// after creation) so the swap chain stays attached to the window for
    /// the surface's lifetime, exactly like the pin's DirectXRenderer
    /// holds its DirectComposition field.
    #[expect(dead_code)]
    composition: Option<CompositionTree>,
    submissions: HashMap<u64, SubmissionRecord>,
    next_seq: u64,
}

struct SurfaceSlot {
    /// Slot generation of the last surface that occupied this slot
    /// (persists while vacant so destroyed handles stay stale after slot
    /// reuse).
    slot_generation: u32,
    occupied: bool,
    surface: Option<SurfaceState>,
}

impl SurfaceSlot {
    fn vacant() -> Self {
        SurfaceSlot { slot_generation: 0, occupied: false, surface: None }
    }
}

/// The whole renderer service state behind one mutex. The mutex IS the
/// immediate-context serialization: every export takes it, and no lock is
/// ever held across a callback (the service has none).
struct RendererState {
    device: Option<DeviceShared>,
    /// Ticket16: the device-global draw state (pipelines and global
    /// elements), created lazily at the first scene draw and leased out
    /// for each draw so instance-buffer updates never alias the state
    /// mutex's other borrows.
    draw: Option<draw::DrawGlobal>,
    slots: Vec<SurfaceSlot>,
}

static GLOBAL: Mutex<RendererState> =
    Mutex::new(RendererState { device: None, draw: None, slots: Vec::new() });

// SAFETY: the renderer state contains raw COM interface wrappers, which
// the windows crate conservatively treats as !Send (COM objects may be
// apartment-affine). The D3D11 device/context here are created with
// multithread protection and every access — device, surface, submission —
// is serialized behind the GLOBAL mutex, so no COM object is ever used
// concurrently; the state merely resides behind a lock reachable from any
// thread. D3D11 and DXGI objects are free-threaded under these
// conditions, making the Send impl sound.
unsafe impl Send for RendererState {}

fn lock_global() -> std::sync::MutexGuard<'static, RendererState> {
    GLOBAL.lock().unwrap_or_else(|poisoned| poisoned.into_inner())
}

/// The COM handles of the shared device, cloned out (AddRef) so callers
/// can use them without holding a borrow of the global state. The atlas
/// service (ticket10) receives these exactly where the pin passes the
/// renderer's device/context into `DirectXAtlas::new`
/// (crates/gpui_windows/src/directx_renderer.rs:349).
#[derive(Clone)]
pub(super) struct DeviceHandles {
    dxgi_factory: IDXGIFactory6,
    /// Kept for the device-info facts path (adapter description probes);
    /// not every caller dereferences it.
    #[expect(dead_code)]
    adapter: IDXGIAdapter1,
    pub(super) device: ID3D11Device,
    pub(super) context: ID3D11DeviceContext,
}

/// Locks the renderer state and returns the shared device handles,
/// creating them on first use. Ticket10's atlas service calls this at
/// atlas creation and at device-loss rebind; the renderer's own entries
/// never take the atlas lock, so the lock order (atlas → renderer) is
/// acyclic.
pub(super) fn shared_device() -> Result<DeviceHandles, i32> {
    let mut state = lock_global();
    ensure_device(&mut state)
}

// ---------------------------------------------------------------------------
// Device creation (pinned directx_devices.rs)
// ---------------------------------------------------------------------------

/// Whether the D3D11 debug layer is available. The pin only probes in
/// debug builds; release builds (the shipped artifact) never do.
#[inline]
fn check_debug_layer_available() -> bool {
    #[cfg(debug_assertions)]
    {
        use windows::Win32::Graphics::Dxgi::{DXGIGetDebugInterface1, IDXGIInfoQueue};
        unsafe { DXGIGetDebugInterface1::<IDXGIInfoQueue>(0) }.is_ok()
    }
    #[cfg(not(debug_assertions))]
    {
        false
    }
}

/// The pin's `get_dxgi_factory`.
#[inline]
fn get_dxgi_factory(debug_layer_available: bool) -> Result<IDXGIFactory6, i32> {
    let factory_flag = if debug_layer_available {
        DXGI_CREATE_FACTORY_DEBUG
    } else {
        DXGI_CREATE_FACTORY_FLAGS::default()
    };
    unsafe { CreateDXGIFactory2(factory_flag) }.map_err(|e| e.code().0)
}

/// The pin's `get_device`: `D3D11CreateDevice` for one adapter with the
/// pinned flags and feature levels.
#[inline]
fn get_device(
    adapter: &IDXGIAdapter1,
    debug_layer_available: bool,
) -> Result<(ID3D11Device, ID3D11DeviceContext, D3D_FEATURE_LEVEL), i32> {
    let mut device: Option<ID3D11Device> = None;
    let mut context: Option<ID3D11DeviceContext> = None;
    let mut feature_level = D3D_FEATURE_LEVEL::default();
    let device_flags = if debug_layer_available {
        D3D11_CREATE_DEVICE_BGRA_SUPPORT | D3D11_CREATE_DEVICE_DEBUG
    } else {
        D3D11_CREATE_DEVICE_BGRA_SUPPORT
    };
    // The generated shader corpus is Shader Model 5.0: the pin restricts
    // device creation to feature levels that can execute it.
    unsafe {
        D3D11CreateDevice(
            adapter,
            D3D_DRIVER_TYPE_UNKNOWN,
            HMODULE::default(),
            device_flags,
            Some(&FEATURE_LEVELS),
            D3D11_SDK_VERSION,
            Some(&mut device),
            Some(&mut feature_level),
            Some(&mut context),
        )
    }
    .map_err(|e| e.code().0)?;
    let device = device.ok_or(renderer_status::ERR_DEVICE)?;
    let context = context.ok_or(renderer_status::ERR_DEVICE)?;
    Ok((device, context, feature_level))
}

/// The pin's `get_adapter` loop: enumerate adapters until NOT_FOUND, take
/// the first whose device creation succeeds.
fn get_adapter(
    dxgi_factory: &IDXGIFactory6,
    debug_layer_available: bool,
) -> Result<(IDXGIAdapter1, ID3D11Device, ID3D11DeviceContext, D3D_FEATURE_LEVEL), i32> {
    for adapter_index in 0.. {
        let adapter: IDXGIAdapter1 = match unsafe { dxgi_factory.EnumAdapters(adapter_index) } {
            Ok(adapter) => adapter.cast().map_err(|e| e.code().0)?,
            Err(error) if error.code() == DXGI_ERROR_NOT_FOUND => break,
            Err(error) => return Err(error.code().0),
        };
        // The pin logs each adapter's description while enumerating; this
        // service records the chosen adapter's description in the
        // device-info records instead.
        if let Ok((device, context, feature_level)) = get_device(&adapter, debug_layer_available) {
            return Ok((adapter, device, context, feature_level));
        }
    }
    Err(renderer_status::ERR_DEVICE)
}

/// Driver identity, ported from the pin's `gpu_specs`/`nvidia`/`amd`/`dxgi`
/// modules. The vendor mapping and the three probe strategies match the
/// pin; the "Unknown Driver" fallback covers probe failure.
fn driver_name_for(vendor_id: u32) -> String {
    match vendor_id {
        0x10DE => "NVIDIA Corporation".to_string(),
        0x1002 => "AMD Corporation".to_string(),
        0x8086 => "Intel Corporation".to_string(),
        id => format!("Unknown Vendor (ID: {:#X})", id),
    }
}

fn driver_version_for(adapter: &IDXGIAdapter1, vendor_id: u32) -> String {
    match vendor_id {
        0x10DE => nvidia::get_driver_version(),
        0x1002 => amd::get_driver_version(),
        _ => dxgi::get_driver_version(adapter),
    }
    .unwrap_or_else(|_| "Unknown Driver".to_string())
}

/// The pin's `with_dll_library`: load, run, free — best-effort free.
fn with_dll_library<R>(
    dll_name: windows::core::PCSTR,
    f: impl FnOnce(HMODULE) -> Result<R, i32>,
) -> Result<R, i32> {
    let library = unsafe { LoadLibraryA(dll_name) }.map_err(|e| e.code().0)?;
    let result = f(library);
    // Best effort, like the pin's logged free.
    drop(unsafe { FreeLibrary(library) });
    result
}

mod nvidia {
    use std::ffi::CStr;
    use std::os::raw::{c_char, c_int, c_uint};

    use windows::Win32::System::LibraryLoader::GetProcAddress;
    use windows::core::s;

    use super::with_dll_library;

    // https://github.com/NVIDIA/nvapi/blob/7cb76fce2f52de818b3da497af646af1ec16ce27/nvapi_lite_common.h#L180
    const NVAPI_SHORT_STRING_MAX: usize = 64;

    // https://github.com/NVIDIA/nvapi/blob/7cb76fce2f52de818b3da497af646af1ec16ce27/nvapi_lite_common.h#L235
    #[allow(non_camel_case_types)]
    type NvAPI_ShortString = [c_char; NVAPI_SHORT_STRING_MAX];

    // https://github.com/NVIDIA/nvapi/blob/7cb76fce2f52de818b3da497af646af1ec16ce27/nvapi_lite_common.h#L447
    #[allow(non_camel_case_types)]
    type NvAPI_SYS_GetDriverAndBranchVersion_t = unsafe extern "C" fn(
        driver_version: *mut c_uint,
        build_branch_string: *mut NvAPI_ShortString,
    ) -> c_int;

    pub(super) fn get_driver_version() -> Result<String, i32> {
        #[cfg(target_pointer_width = "64")]
        let nvidia_dll_name = s!("nvapi64.dll");
        #[cfg(target_pointer_width = "32")]
        let nvidia_dll_name = s!("nvapi.dll");

        with_dll_library(nvidia_dll_name, |nvidia_dll| unsafe {
            let nvapi_query_addr = GetProcAddress(nvidia_dll, s!("nvapi_QueryInterface"))
                .ok_or(-1_i32)?;
            let nvapi_query: extern "C" fn(u32) -> *mut () =
                std::mem::transmute(nvapi_query_addr);

            // https://github.com/NVIDIA/nvapi/blob/7cb76fce2f52de818b3da497af646af1ec16ce27/nvapi_interface.h#L41
            let nvapi_get_driver_version_ptr = nvapi_query(0x2926aaad);
            if nvapi_get_driver_version_ptr.is_null() {
                return Err(-1_i32);
            }
            let nvapi_get_driver_version: NvAPI_SYS_GetDriverAndBranchVersion_t =
                std::mem::transmute(nvapi_get_driver_version_ptr);

            let mut driver_version: c_uint = 0;
            let mut build_branch_string: NvAPI_ShortString = [0; NVAPI_SHORT_STRING_MAX];
            let result = nvapi_get_driver_version(
                &mut driver_version as *mut c_uint,
                &mut build_branch_string as *mut NvAPI_ShortString,
            );

            if result != 0 {
                return Err(result);
            }
            let major = driver_version / 100;
            let minor = driver_version % 100;
            let branch_string = CStr::from_ptr(build_branch_string.as_ptr());
            Ok(format!("{}.{} {}", major, minor, branch_string.to_string_lossy()))
        })
    }
}

mod amd {
    use std::os::raw::{c_char, c_int, c_void};

    use windows::Win32::System::LibraryLoader::GetProcAddress;
    use windows::core::s;

    use super::with_dll_library;

    // https://github.com/GPUOpen-LibrariesAndSDKs/AGS_SDK/blob/5d8812d703d0335741b6f7ffc37838eeb8b967f7/ags_lib/inc/amd_ags.h#L145
    const AGS_CURRENT_VERSION: i32 = (6 << 22) | (3 << 12);

    // https://github.com/GPUOpen-LibrariesAndSDKs/AGS_SDK/blob/5d8812d703d0335741b6f7ffc37838eeb8b967f7/ags_lib/inc/amd_ags.h#L204
    #[repr(C)]
    struct AGSContext {
        _private: [u8; 0],
    }

    #[repr(C)]
    pub struct AGSGPUInfo {
        pub driver_version: *const c_char,
        pub radeon_software_version: *const c_char,
        pub num_devices: c_int,
        pub devices: *mut c_void,
    }

    // https://github.com/GPUOpen-LibrariesAndSDKs/AGS_SDK/blob/5d8812d703d0335741b6f7ffc37838eeb8b967f7/ags_lib/inc/amd_ags.h#L429
    #[allow(non_camel_case_types)]
    type agsInitialize_t = unsafe extern "C" fn(
        version: c_int,
        config: *const c_void,
        context: *mut *mut AGSContext,
        gpu_info: *mut AGSGPUInfo,
    ) -> c_int;

    // https://github.com/GPUOpen-LibrariesAndSDKs/AGS_SDK/blob/5d8812d703d0335741b6f7ffc37838eeb8b967f7/ags_lib/inc/amd_ags.h#L436
    #[allow(non_camel_case_types)]
    type agsDeInitialize_t = unsafe extern "C" fn(context: *mut AGSContext) -> c_int;

    pub(super) fn get_driver_version() -> Result<String, i32> {
        #[cfg(target_pointer_width = "64")]
        let amd_dll_name = s!("amd_ags_x64.dll");
        #[cfg(target_pointer_width = "32")]
        let amd_dll_name = s!("amd_ags_x86.dll");

        with_dll_library(amd_dll_name, |amd_dll| unsafe {
            let ags_initialize_addr =
                GetProcAddress(amd_dll, s!("agsInitialize")).ok_or(-1_i32)?;
            let ags_deinitialize_addr =
                GetProcAddress(amd_dll, s!("agsDeInitialize")).ok_or(-1_i32)?;

            let ags_initialize: agsInitialize_t = std::mem::transmute(ags_initialize_addr);
            let ags_deinitialize: agsDeInitialize_t = std::mem::transmute(ags_deinitialize_addr);

            let mut context: *mut AGSContext = std::ptr::null_mut();
            let mut gpu_info: AGSGPUInfo = AGSGPUInfo {
                driver_version: std::ptr::null(),
                radeon_software_version: std::ptr::null(),
                num_devices: 0,
                devices: std::ptr::null_mut(),
            };

            let result = ags_initialize(
                AGS_CURRENT_VERSION,
                std::ptr::null(),
                &mut context,
                &mut gpu_info,
            );
            if result != 0 {
                return Err(result);
            }

            // Vulkan actually returns this as the driver version
            let software_version = if !gpu_info.radeon_software_version.is_null() {
                std::ffi::CStr::from_ptr(gpu_info.radeon_software_version)
                    .to_string_lossy()
                    .into_owned()
            } else {
                "Unknown Radeon Software Version".to_string()
            };

            let driver_version = if !gpu_info.driver_version.is_null() {
                std::ffi::CStr::from_ptr(gpu_info.driver_version)
                    .to_string_lossy()
                    .into_owned()
            } else {
                "Unknown Radeon Driver Version".to_string()
            };

            ags_deinitialize(context);
            Ok(format!("{} ({})", software_version, driver_version))
        })
    }
}

mod dxgi {
    use windows::Win32::Graphics::Dxgi::{IDXGIAdapter1, IDXGIDevice};
    use windows::core::Interface;

    pub(super) fn get_driver_version(adapter: &IDXGIAdapter1) -> Result<String, i32> {
        let number = unsafe {
            adapter.CheckInterfaceSupport(&IDXGIDevice::IID as *const windows::core::GUID)
        }
        .map_err(|e| e.code().0)?;
        Ok(format!(
            "{}.{}.{}.{}",
            number >> 48,
            (number >> 32) & 0xFFFF,
            (number >> 16) & 0xFFFF,
            number & 0xFFFF
        ))
    }
}

/// Creates the shared device on first use and returns its COM handles
/// (cloned, AddRef'd). Callers must hold the global lock.
fn ensure_device(state: &mut RendererState) -> Result<DeviceHandles, i32> {
    if let Some(device) = state.device.as_ref() {
        return Ok(DeviceHandles {
            dxgi_factory: device.dxgi_factory.clone(),
            adapter: device.adapter.clone(),
            device: device.device.clone(),
            context: device.context.clone(),
        });
    }
    let debug_layer_available = check_debug_layer_available();
    let dxgi_factory = get_dxgi_factory(debug_layer_available)?;
    let (adapter, device, context, feature_level) =
        get_adapter(&dxgi_factory, debug_layer_available)?;

    // Multithread protection on the immediate context, per the renderer
    // contract ("Set and verify immediate-context multithread protection");
    // the return value reports the PREVIOUS setting, recorded in the facts.
    let multithread: ID3D11Multithread = context.cast().map_err(|e| e.code().0)?;
    let _previous = unsafe { multithread.SetMultithreadProtected(true) };

    let desc = unsafe { adapter.GetDesc1() }.map_err(|e| e.code().0)?;
    let software = (desc.Flags & DXGI_ADAPTER_FLAG_SOFTWARE.0 as u32) != 0;
    let adapter_desc = String::from_utf16_lossy(&desc.Description)
        .trim_matches(char::from(0))
        .to_string();
    let driver_name = driver_name_for(desc.VendorId);
    let driver_version = driver_version_for(&adapter, desc.VendorId);

    state.device = Some(DeviceShared {
        dxgi_factory: dxgi_factory.clone(),
        adapter: adapter.clone(),
        device: device.clone(),
        context: context.clone(),
        facts: DeviceFacts {
            vendor_id: desc.VendorId,
            device_id: desc.DeviceId,
            software,
            dedicated_video_memory: desc.DedicatedVideoMemory as u64,
            feature_level: feature_level.0 as u32,
            adapter_desc,
            driver_name,
            driver_version,
            multithread_protected: true,
        },
    });
    Ok(DeviceHandles { dxgi_factory, adapter, device, context })
}

// ---------------------------------------------------------------------------
// Swap chain creation (pinned directx_renderer.rs)
// ---------------------------------------------------------------------------

/// The pin's `create_swap_chain_for_composition` (mode 0).
fn create_swap_chain_for_composition(
    dxgi_factory: &IDXGIFactory6,
    device: &ID3D11Device,
    width: u32,
    height: u32,
) -> Result<IDXGISwapChain1, i32> {
    let desc = DXGI_SWAP_CHAIN_DESC1 {
        Width: width,
        Height: height,
        Format: DXGI_FORMAT_B8G8R8A8_UNORM,
        Stereo: false.into(),
        SampleDesc: DXGI_SAMPLE_DESC { Count: 1, Quality: 0 },
        BufferUsage: DXGI_USAGE_RENDER_TARGET_OUTPUT,
        BufferCount: RENDERER_BUFFER_COUNT,
        // Composition SwapChains only support the DXGI_SCALING_STRETCH Scaling.
        Scaling: DXGI_SCALING_STRETCH,
        SwapEffect: DXGI_SWAP_EFFECT_FLIP_SEQUENTIAL,
        AlphaMode: DXGI_ALPHA_MODE_PREMULTIPLIED,
        Flags: 0,
    };
    unsafe { dxgi_factory.CreateSwapChainForComposition(device, &desc, None) }
        .map_err(|e| e.code().0)
}

/// The pin's `create_swap_chain` (mode 1, DirectComposition disabled).
fn create_swap_chain(
    dxgi_factory: &IDXGIFactory6,
    device: &ID3D11Device,
    hwnd: HWND,
    width: u32,
    height: u32,
) -> Result<IDXGISwapChain1, i32> {
    let desc = DXGI_SWAP_CHAIN_DESC1 {
        Width: width,
        Height: height,
        Format: DXGI_FORMAT_B8G8R8A8_UNORM,
        Stereo: false.into(),
        SampleDesc: DXGI_SAMPLE_DESC { Count: 1, Quality: 0 },
        BufferUsage: DXGI_USAGE_RENDER_TARGET_OUTPUT,
        BufferCount: RENDERER_BUFFER_COUNT,
        Scaling: DXGI_SCALING_NONE,
        SwapEffect: DXGI_SWAP_EFFECT_FLIP_SEQUENTIAL,
        AlphaMode: DXGI_ALPHA_MODE_IGNORE,
        Flags: 0,
    };
    let swap_chain =
        unsafe { dxgi_factory.CreateSwapChainForHwnd(device, hwnd, &desc, None, None) }
            .map_err(|e| e.code().0)?;
    unsafe { dxgi_factory.MakeWindowAssociation(hwnd, DXGI_MWA_NO_ALT_ENTER) }
        .map_err(|e| e.code().0)?;
    Ok(swap_chain)
}

/// The pin's `create_render_target_and_its_view`.
fn create_render_target_and_its_view(
    swap_chain: &IDXGISwapChain1,
    device: &ID3D11Device,
) -> Result<(ID3D11Texture2D, Option<ID3D11RenderTargetView>), i32> {
    let render_target: ID3D11Texture2D =
        unsafe { swap_chain.GetBuffer(0) }.map_err(|e| e.code().0)?;
    let mut render_target_view = None;
    unsafe { device.CreateRenderTargetView(&render_target, None, Some(&mut render_target_view)) }
        .map_err(|e| e.code().0)?;
    Ok((render_target, render_target_view))
}

/// The pin's `set_viewport`.
fn set_viewport(device_context: &ID3D11DeviceContext, width: f32, height: f32) -> D3D11_VIEWPORT {
    let viewport = [D3D11_VIEWPORT {
        TopLeftX: 0.0,
        TopLeftY: 0.0,
        Width: width,
        Height: height,
        MinDepth: 0.0,
        MaxDepth: 1.0,
    }];
    unsafe { device_context.RSSetViewports(Some(&viewport)) };
    viewport[0]
}

/// The pin's `DirectComposition::new` + `set_swap_chain`.
fn create_composition_tree(
    device: &ID3D11Device,
    hwnd: HWND,
    swap_chain: &IDXGISwapChain1,
) -> Result<CompositionTree, i32> {
    let dxgi_device: IDXGIDevice = device.cast().map_err(|e| e.code().0)?;
    let comp_device: IDCompositionDevice =
        unsafe { DCompositionCreateDevice(&dxgi_device) }.map_err(|e| e.code().0)?;
    let comp_target =
        unsafe { comp_device.CreateTargetForHwnd(hwnd, true) }.map_err(|e| e.code().0)?;
    let comp_visual = unsafe { comp_device.CreateVisual() }.map_err(|e| e.code().0)?;
    unsafe {
        comp_visual.SetContent(swap_chain).map_err(|e| e.code().0)?;
        comp_target.SetRoot(&comp_visual).map_err(|e| e.code().0)?;
        comp_device.Commit().map_err(|e| e.code().0)?;
    }
    Ok(CompositionTree { comp_device, comp_target, comp_visual })
}

// ---------------------------------------------------------------------------
// Device-info string packing
// ---------------------------------------------------------------------------

/// Packs the three device-info strings contiguously (adapter description,
/// driver name, driver version, in that order) into `buf` and returns the
/// record with offsets/lengths filled. When `buf` is too small, returns
/// the required byte count instead (the `ERR_CAPACITY` protocol).
fn pack_device_info(facts: &DeviceFacts, buf: &mut [u8]) -> (Option<GpuiGoDeviceInfoRecord>, u32) {
    let desc = facts.adapter_desc.as_bytes();
    let name = facts.driver_name.as_bytes();
    let version = facts.driver_version.as_bytes();
    let needed = desc.len() + name.len() + version.len();
    if buf.len() < needed {
        return (None, needed as u32);
    }
    let mut cursor = 0usize;
    let write = |bytes: &[u8], buf: &mut [u8], cursor: &mut usize| -> (u32, u32) {
        let offset = *cursor;
        buf[offset..offset + bytes.len()].copy_from_slice(bytes);
        *cursor += bytes.len();
        (offset as u32, bytes.len() as u32)
    };
    let (desc_off, desc_len) = write(desc, buf, &mut cursor);
    let (name_off, name_len) = write(name, buf, &mut cursor);
    let (version_off, version_len) = write(version, buf, &mut cursor);
    let record = GpuiGoDeviceInfoRecord {
        vendor_id: facts.vendor_id,
        device_id: facts.device_id,
        feature_level: facts.feature_level,
        adapter_flags: u32::from(facts.software),
        reserved: 0,
        dedicated_video_memory: facts.dedicated_video_memory,
        multithread_protected: u32::from(facts.multithread_protected),
        adapter_desc_offset: desc_off,
        adapter_desc_len: desc_len,
        driver_name_offset: name_off,
        driver_name_len: name_len,
        driver_version_offset: version_off,
        driver_version_len: version_len,
        record_size: std::mem::size_of::<GpuiGoDeviceInfoRecord>() as u32,
    };
    (Some(record), needed as u32)
}

// ---------------------------------------------------------------------------
// Service table
// ---------------------------------------------------------------------------

/// The renderer service table installed in reserved slot 2 of
/// `GpuiGoAbiTable`. All 8 function pointers come first (64 bytes), then 8
/// self-check scalars (32 bytes). Offsets (x86-64): pointers @0..64,
/// `service_version` @64, `size_of_table` @68, `align_of_table` @72,
/// `size_of_surface_info` @76, `size_of_submission_state` @80,
/// `size_of_retire_record` @84, `size_of_device_info` @88,
/// `max_pending_submissions` @92; total size 96, alignment 8.
#[repr(C)]
pub struct GpuiGoRendererTable {
    /// `renderer_surface_create`.
    pub surface_create: Option<
        unsafe extern "system" fn(hwnd: u64, mode: u32, width: u32, height: u32, scale_bits: u32, out_surface: *mut u64) -> i32,
    >,
    /// `renderer_surface_resize`.
    pub surface_resize: Option<unsafe extern "system" fn(surface: u64, width: u32, height: u32) -> i32>,
    /// `renderer_surface_info`.
    pub surface_info: Option<unsafe extern "system" fn(surface: u64, out: *mut GpuiGoSurfaceInfoRecord) -> i32>,
    /// `renderer_surface_present`.
    pub surface_present: Option<unsafe extern "system" fn(surface: u64, rgba_bits: *const u32, out_submission: *mut u64) -> i32>,
    /// `renderer_surface_destroy`.
    pub surface_destroy: Option<unsafe extern "system" fn(surface: u64) -> i32>,
    /// `renderer_submission_poll`.
    pub submission_poll: Option<unsafe extern "system" fn(submission: u64, out: *mut GpuiGoSubmissionStateRecord) -> i32>,
    /// `renderer_submission_retire`.
    pub submission_retire: Option<unsafe extern "system" fn(submission: u64, out: *mut GpuiGoRetireRecord) -> i32>,
    /// `renderer_device_info`.
    pub device_info: Option<
        unsafe extern "system" fn(out: *mut GpuiGoDeviceInfoRecord, buf: *mut u8, buf_len: u32, out_needed: *mut u32) -> i32,
    >,
    /// `renderer_surface_draw_scene` (ticket16): render a finished scene
    /// into the surface, present, and return the tracked submission
    /// handle (the pin's `DirectXRenderer::draw`).
    pub surface_draw_scene: Option<
        unsafe extern "system" fn(u64, u64, u32, u64, *mut u64) -> i32,
    >,
    /// `renderer_surface_render_scene` (ticket16): render without
    /// presenting (the pin's `render`; the readback test path).
    pub surface_render_scene: Option<unsafe extern "system" fn(u64, u64, u32, u64) -> i32>,
    /// `renderer_surface_read_pixels` (ticket16): staging readback of
    /// the surface's back buffer (the pin's `render_to_image` tail).
    pub surface_read_pixels: Option<
        unsafe extern "system" fn(u64, *mut u8, u32, *mut u32) -> i32,
    >,
    /// Renderer ABI version (see [`GPUI_GO_RENDERER_SERVICE_VERSION`]).
    pub service_version: u32,
    /// `size_of::<GpuiGoRendererTable>()` self-check.
    pub size_of_table: u32,
    /// `align_of::<GpuiGoRendererTable>()` self-check.
    pub align_of_table: u32,
    /// `size_of::<GpuiGoSurfaceInfoRecord>()` self-check.
    pub size_of_surface_info: u32,
    /// `size_of::<GpuiGoSubmissionStateRecord>()` self-check.
    pub size_of_submission_state: u32,
    /// `size_of::<GpuiGoRetireRecord>()` self-check.
    pub size_of_retire_record: u32,
    /// `size_of::<GpuiGoDeviceInfoRecord>()` self-check.
    pub size_of_device_info: u32,
    /// [`MAX_PENDING_SUBMISSIONS`] — the bounded pending-submission cap,
    /// mirrored for the Go-side backpressure check.
    pub max_pending_submissions: u32,
    /// [`MAX_INSTANCE_BUFFER_SIZE`] — the instance-buffer byte ceiling
    /// (the pin's limit, mirrored for the Go side).
    pub max_instance_buffer_bytes: u32,
    /// [`PATH_MULTISAMPLE_COUNT`] — the path MSAA sample count (the
    /// pin's, mirrored for the Go side).
    pub path_multisample_count: u32,
}

pub(crate) static RENDERER_TABLE: GpuiGoRendererTable = GpuiGoRendererTable {
    surface_create: Some(renderer_surface_create),
    surface_resize: Some(renderer_surface_resize),
    surface_info: Some(renderer_surface_info),
    surface_present: Some(renderer_surface_present),
    surface_destroy: Some(renderer_surface_destroy),
    submission_poll: Some(renderer_submission_poll),
    submission_retire: Some(renderer_submission_retire),
    device_info: Some(renderer_device_info),
    surface_draw_scene: Some(renderer_surface_draw_scene),
    surface_render_scene: Some(renderer_surface_render_scene),
    surface_read_pixels: Some(renderer_surface_read_pixels),
    service_version: GPUI_GO_RENDERER_SERVICE_VERSION,
    size_of_table: std::mem::size_of::<GpuiGoRendererTable>() as u32,
    align_of_table: std::mem::align_of::<GpuiGoRendererTable>() as u32,
    size_of_surface_info: std::mem::size_of::<GpuiGoSurfaceInfoRecord>() as u32,
    size_of_submission_state: std::mem::size_of::<GpuiGoSubmissionStateRecord>() as u32,
    size_of_retire_record: std::mem::size_of::<GpuiGoRetireRecord>() as u32,
    size_of_device_info: std::mem::size_of::<GpuiGoDeviceInfoRecord>() as u32,
    max_pending_submissions: MAX_PENDING_SUBMISSIONS,
    max_instance_buffer_bytes: MAX_INSTANCE_BUFFER_SIZE as u32,
    path_multisample_count: PATH_MULTISAMPLE_COUNT,
};

// ---------------------------------------------------------------------------
// Resolution helpers (caller holds the global lock)
// ---------------------------------------------------------------------------

fn resolve_slot(
    state: &RendererState,
    slot_index: usize,
    slot_generation: u32,
) -> Result<&SurfaceState, i32> {
    if slot_index >= state.slots.len() {
        return Err(renderer_status::ERR_BAD_HANDLE);
    }
    let slot = &state.slots[slot_index];
    if !slot.occupied || slot.slot_generation != slot_generation {
        return Err(renderer_status::ERR_STALE_HANDLE);
    }
    slot.surface.as_ref().ok_or(renderer_status::ERR_STALE_HANDLE)
}

fn resolve_slot_mut(
    state: &mut RendererState,
    slot_index: usize,
    slot_generation: u32,
) -> Result<&mut SurfaceState, i32> {
    if slot_index >= state.slots.len() {
        return Err(renderer_status::ERR_BAD_HANDLE);
    }
    let slot = &mut state.slots[slot_index];
    if !slot.occupied || slot.slot_generation != slot_generation {
        return Err(renderer_status::ERR_STALE_HANDLE);
    }
    slot.surface.as_mut().ok_or(renderer_status::ERR_STALE_HANDLE)
}

/// Resolves a submission handle to its record with an immutable borrow.
/// The slot + generation are checked against the registry; the sequence is
/// checked against the surface's map (retired records are absent).
fn find_submission(state: &RendererState, handle: u64) -> Result<&SubmissionRecord, i32> {
    let (slot_index, slot_generation, sequence) = submission_handle_parts(handle);
    let surface = resolve_slot(state, slot_index, slot_generation)?;
    if sequence >= surface.next_seq {
        return Err(renderer_status::ERR_BAD_HANDLE);
    }
    match surface.submissions.get(&sequence) {
        Some(record) => Ok(record),
        // Issued once but no longer present: retired.
        None => Err(renderer_status::ERR_STALE_HANDLE),
    }
}

/// Resolves a submission handle to its record with a mutable borrow
/// (poll state transitions). Same validation as find_submission.
fn find_submission_mut(state: &mut RendererState, handle: u64) -> Result<&mut SubmissionRecord, i32> {
    let (slot_index, slot_generation, sequence) = submission_handle_parts(handle);
    if slot_index >= state.slots.len() {
        return Err(renderer_status::ERR_BAD_HANDLE);
    }
    let slot = &mut state.slots[slot_index];
    if !slot.occupied || slot.slot_generation != slot_generation {
        return Err(renderer_status::ERR_STALE_HANDLE);
    }
    let surface = slot.surface.as_mut().ok_or(renderer_status::ERR_STALE_HANDLE)?;
    if sequence >= surface.next_seq {
        return Err(renderer_status::ERR_BAD_HANDLE);
    }
    match surface.submissions.get_mut(&sequence) {
        Some(record) => Ok(record),
        None => Err(renderer_status::ERR_STALE_HANDLE),
    }
}

// ---------------------------------------------------------------------------
// Export bodies
// ---------------------------------------------------------------------------

fn surface_create_body(
    hwnd: u64,
    mode: u32,
    width: u32,
    height: u32,
    scale_bits: u32,
    out_surface: *mut u64,
) -> i32 {
    if out_surface.is_null() {
        return renderer_status::ERR_NULL_ARG;
    }
    if mode != surface_mode::DCOMP_PREMULTIPLIED && mode != surface_mode::HWND_ALPHA_IGNORE {
        return renderer_status::ERR_BAD_VALUE;
    }
    if width > MAX_SURFACE_EXTENT || height > MAX_SURFACE_EXTENT {
        return renderer_status::ERR_BAD_VALUE;
    }
    // The pin creates swap chains at >= 1x1 and clamps resizes to >= 1.
    let width = width.max(1);
    let height = height.max(1);

    let mut state = lock_global();
    let device = match ensure_device(&mut state) {
        Ok(device) => device,
        Err(code) => return code,
    };
    let slot_index = match state.slots.iter().position(|slot| !slot.occupied) {
        Some(index) => index,
        None => {
            state.slots.push(SurfaceSlot::vacant());
            state.slots.len() - 1
        }
    };
    if slot_index as u32 > SUBMISSION_SLOT_MAX {
        return renderer_status::ERR_BAD_VALUE;
    }
    let slot_generation = match state.slots[slot_index].slot_generation.checked_add(1) {
        Some(generation) if generation <= SURFACE_GENERATION_MAX => generation,
        _ => return renderer_status::ERR_BAD_VALUE,
    };

    let hwnd_handle = HWND(hwnd as *mut core::ffi::c_void);
    let swap_chain = match mode {
        surface_mode::DCOMP_PREMULTIPLIED => create_swap_chain_for_composition(
            &device.dxgi_factory,
            &device.device,
            width,
            height,
        ),
        _ => create_swap_chain(&device.dxgi_factory, &device.device, hwnd_handle, width, height),
    };
    let swap_chain = match swap_chain {
        Ok(chain) => chain,
        Err(code) => return code,
    };

    let (render_target, render_target_view) =
        match create_render_target_and_its_view(&swap_chain, &device.device) {
            Ok(pair) => pair,
            Err(code) => return code,
        };
    let viewport = set_viewport(&device.context, width as f32, height as f32);

    let composition = match mode {
        surface_mode::DCOMP_PREMULTIPLIED => {
            match create_composition_tree(&device.device, hwnd_handle, &swap_chain) {
                Ok(tree) => Some(tree),
                Err(code) => return code,
            }
        }
        _ => None,
    };

    // Bind the render target like the pin's DirectXResources::new tail.
    unsafe {
        device
            .context
            .OMSetRenderTargets(Some(slice::from_ref(&render_target_view)), None);
    }

    state.slots[slot_index] = SurfaceSlot {
        slot_generation,
        occupied: true,
        surface: Some(SurfaceState {
            hwnd,
            mode,
            width,
            height,
            scale_bits,
            resize_generation: 0,
            swap_chain,
            render_target: Some(render_target),
            render_target_view,
            viewport,
            draw_targets: draw::DrawTargets::default(),
            composition,
            submissions: HashMap::new(),
            next_seq: 0,
        }),
    };
    unsafe { *out_surface = make_surface_handle(slot_index, slot_generation) };
    renderer_status::OK
}

/// Extent bound: large enough for any display, small enough to reject
/// garbage before it reaches DXGI.
const MAX_SURFACE_EXTENT: u32 = 1 << 20;

fn surface_resize_body(surface: u64, width: u32, height: u32) -> i32 {
    if width > MAX_SURFACE_EXTENT || height > MAX_SURFACE_EXTENT {
        return renderer_status::ERR_BAD_VALUE;
    }
    let (slot_index, slot_generation) = surface_handle_parts(surface);
    let width = width.max(1);
    let height = height.max(1);

    let mut state = lock_global();
    let device = match ensure_device(&mut state) {
        Ok(device) => device,
        Err(code) => return code,
    };
    let surface_state = match resolve_slot_mut(&mut state, slot_index, slot_generation) {
        Ok(surface) => surface,
        Err(code) => return code,
    };
    if surface_state.width == width && surface_state.height == height {
        // The pin's resize returns early on unchanged extents.
        return renderer_status::OK;
    }
    surface_state.width = width;
    surface_state.height = height;

    // Clear the render target before resizing (the pin's order).
    unsafe { device.context.OMSetRenderTargets(None, None) };
    surface_state.render_target.take();
    surface_state.render_target_view.take();

    // Resizing the swap chain can return device-removed (the window moved
    // to a monitor on another graphics device); surface the error, per
    // the pin.
    unsafe {
        if let Err(e) = surface_state.swap_chain.ResizeBuffers(
            RENDERER_BUFFER_COUNT,
            width,
            height,
            DXGI_FORMAT_B8G8R8A8_UNORM,
            DXGI_SWAP_CHAIN_FLAG(0),
        ) {
            return e.code().0;
        }
    }

    let (render_target, render_target_view) =
        match create_render_target_and_its_view(&surface_state.swap_chain, &device.device) {
            Ok(pair) => pair,
            Err(code) => return code,
        };
    let viewport = set_viewport(&device.context, width as f32, height as f32);
    surface_state.render_target = Some(render_target);
    surface_state.render_target_view = render_target_view;
    surface_state.viewport = viewport;
    // Intermediate textures are size-dependent and recreated lazily if
    // a later scene needs them (the pin's `recreate_resources`).
    surface_state.draw_targets = draw::DrawTargets::default();
    surface_state.resize_generation = surface_state
        .resize_generation
        .checked_add(1)
        .unwrap_or(u32::MAX);

    unsafe {
        device
            .context
            .OMSetRenderTargets(Some(slice::from_ref(&surface_state.render_target_view)), None);
    }
    renderer_status::OK
}

fn surface_info_body(surface: u64, out: *mut GpuiGoSurfaceInfoRecord) -> i32 {
    if out.is_null() {
        return renderer_status::ERR_NULL_ARG;
    }
    let (slot_index, slot_generation) = surface_handle_parts(surface);
    let state = lock_global();
    let surface = match resolve_slot(&state, slot_index, slot_generation) {
        Ok(surface) => surface,
        Err(code) => return code,
    };
    let feature_level = state
        .device
        .as_ref()
        .map(|device| device.facts.feature_level)
        .unwrap_or(0);
    unsafe {
        *out = GpuiGoSurfaceInfoRecord {
            mode: surface.mode,
            format: RENDER_TARGET_FORMAT_VALUE,
            buffer_count: RENDERER_BUFFER_COUNT,
            width: surface.width,
            height: surface.height,
            feature_level,
            resize_generation: surface.resize_generation,
            scale_bits: surface.scale_bits,
            record_size: std::mem::size_of::<GpuiGoSurfaceInfoRecord>() as u32,
        };
    }
    renderer_status::OK
}

fn surface_present_body(surface: u64, rgba_bits: *const u32, out_submission: *mut u64) -> i32 {
    if rgba_bits.is_null() || out_submission.is_null() {
        return renderer_status::ERR_NULL_ARG;
    }
    let clear_color = unsafe {
        [
            f32::from_bits(*rgba_bits),
            f32::from_bits(*rgba_bits.add(1)),
            f32::from_bits(*rgba_bits.add(2)),
            f32::from_bits(*rgba_bits.add(3)),
        ]
    };

    let (slot_index, slot_generation) = surface_handle_parts(surface);
    let mut state = lock_global();
    let device = match ensure_device(&mut state) {
        Ok(device) => device,
        Err(code) => return code,
    };
    let surface_state = match resolve_slot_mut(&mut state, slot_index, slot_generation) {
        Ok(surface) => surface,
        Err(code) => return code,
    };
    // Bounded pending submissions: backpressure BEFORE accepting GPU work.
    if surface_state.submissions.len() >= MAX_PENDING_SUBMISSIONS as usize {
        return renderer_status::ERR_BUSY;
    }
    // Sequence space exhaustion (2^32 per surface) is a hard error.
    match surface_state.next_seq.checked_add(1) {
        Some(next) if next <= SUBMISSION_SEQ_MASK => {}
        _ => return renderer_status::ERR_BAD_VALUE,
    }
    let render_target_view = match surface_state.render_target_view.as_ref() {
        Some(view) => view,
        None => return renderer_status::ERR_DEVICE,
    };

    // The pin's pre_draw order: clear, then bind targets and viewport.
    // (ClearRenderTargetView does not require the target to be bound.)
    unsafe {
        device.context.ClearRenderTargetView(render_target_view, &clear_color);
        device
            .context
            .OMSetRenderTargets(Some(slice::from_ref(&surface_state.render_target_view)), None);
        device
            .context
            .RSSetViewports(Some(slice::from_ref(&surface_state.viewport)));
    }

    // The pin's present: Present(0, DXGI_PRESENT(0)).
    let present_hr = unsafe { surface_state.swap_chain.Present(0, DXGI_PRESENT(0)) };

    // Completion marker: a D3D11_QUERY_EVENT ended AFTER the last command
    // of the submission (the clear; Present is not an immediate-context
    // command), then ONE explicit Flush so the marker is actually
    // submitted — each submission here is the "rendering goes idle" case
    // of the renderer contract (there is no later frame that would carry
    // it), and the pin's own flush point is its quiescing/loss path.
    // Present and Flush are submission aids, never completion evidence:
    // only a later poll's S_OK retires.
    let query = {
        let desc = D3D11_QUERY_DESC { Query: D3D11_QUERY_EVENT, MiscFlags: 0 };
        let mut query: Option<ID3D11Query> = None;
        match unsafe { device.device.CreateQuery(&desc, Some(&mut query)) } {
            Ok(()) => match query {
                Some(query) => query,
                // The marker could not be created: no acceptance. The clear
                // and present happened, but no per-submission resources
                // are retained past the back buffer, so nothing needs a
                // token.
                None => {
                    if present_hr.is_err() {
                        return renderer_status::ERR_PRESENT;
                    }
                    return renderer_status::ERR_DEVICE;
                }
            },
            Err(e) => {
                let _ = e;
                if present_hr.is_err() {
                    return renderer_status::ERR_PRESENT;
                }
                return renderer_status::ERR_DEVICE;
            }
        }
    };
    unsafe {
        device.context.End(&query);
        // Asynchronous; never an acknowledgment.
        device.context.Flush();
    }

    let sequence = surface_state.next_seq;
    surface_state.next_seq += 1;
    // Partial-submit path: a failed Present still tracks the submission
    // (the contract's "errors after partial GPU work still return/retain a
    // trackable submission token"); polling resolves the real state.
    surface_state.submissions.insert(
        sequence,
        SubmissionRecord {
            sequence,
            query: Some(query),
            state: SubmissionState::Pending,
            present_hr: present_hr.0,
            data_hr: 0,
        },
    );
    let handle = make_submission_handle(slot_index, slot_generation, sequence);
    unsafe { *out_submission = handle };
    if present_hr.is_err() {
        return renderer_status::ERR_PRESENT;
    }
    renderer_status::OK
}

/// Reads the query with the RAW vtable HRESULT: the generated wrapper
/// collapses S_OK and S_FALSE (both positive) into `Ok(())`, but the
/// retirement protocol needs the distinction (S_OK = completed, S_FALSE =
/// pending).
fn poll_get_data_raw(context: &ID3D11DeviceContext, query: &ID3D11Query) -> i32 {
    unsafe {
        (Interface::vtable(context).GetData)(
            Interface::as_raw(context),
            Interface::as_raw(query),
            core::ptr::null_mut(),
            0,
            0,
        )
        .0
    }
}

/// The pin's check_device_lost: Ok = alive, Err(code) = removed (the code
/// is the removal reason).
fn device_removed_reason(device: &ID3D11Device) -> Option<i32> {
    match unsafe { device.GetDeviceRemovedReason() } {
        Ok(()) => None,
        Err(e) => Some(e.code().0),
    }
}

fn submission_poll_body(submission: u64, out: *mut GpuiGoSubmissionStateRecord) -> i32 {
    if out.is_null() {
        return renderer_status::ERR_NULL_ARG;
    }
    let mut state = lock_global();
    // Terminal states are sticky: repeated polls return them without
    // touching the device again.
    let pending_query = {
        let record = match find_submission(&state, submission) {
            Ok(record) => record,
            Err(code) => return code,
        };
        match record.state {
            SubmissionState::Pending => record.query.clone(),
            _ => None,
        }
    };
    if let Some(query) = pending_query {
        let context = match state.device.as_ref() {
            Some(device) => device.context.clone(),
            None => return renderer_status::ERR_DEVICE,
        };
        // One bounded, nonblocking GetData call.
        let hr = poll_get_data_raw(&context, &query);
        let device_probe = state.device.as_ref().map(|device| device.device.clone());
        let record: &mut SubmissionRecord = match find_submission_mut(&mut state, submission) {
            Ok(record) => record,
            Err(code) => return code,
        };
        record.data_hr = hr;
        if hr == 0 {
            // S_OK: the GPU finished the submission's work.
            record.state = SubmissionState::Completed;
        } else if hr > 0 {
            // S_FALSE: still pending. The record stays tracked.
        } else {
            // A query error is not by itself a safe-abort proof: confirm
            // device loss with the pin's check.
            record.state = match device_probe.as_ref().and_then(device_removed_reason) {
                Some(reason) => SubmissionState::Failed { reason },
                None => SubmissionState::Failed { reason: hr },
            };
        }
    }
    let record = match find_submission(&state, submission) {
        Ok(record) => record,
        Err(code) => return code,
    };
    let (state_value, reason) = match record.state {
        SubmissionState::Pending => (submission_state::PENDING, 0),
        SubmissionState::Completed => (submission_state::COMPLETED, 0),
        SubmissionState::Failed { reason } => (submission_state::FAILED, reason),
    };
    unsafe {
        *out = GpuiGoSubmissionStateRecord {
            state: state_value,
            device_removed_reason: reason,
            data_hr: record.data_hr,
            present_hr: record.present_hr,
            record_size: std::mem::size_of::<GpuiGoSubmissionStateRecord>() as u32,
        };
    }
    renderer_status::OK
}

fn submission_retire_body(submission: u64, out: *mut GpuiGoRetireRecord) -> i32 {
    if out.is_null() {
        return renderer_status::ERR_NULL_ARG;
    }
    let (slot_index, slot_generation, sequence) = submission_handle_parts(submission);
    let mut state = lock_global();
    let device_probe = state.device.as_ref().map(|device| device.device.clone());
    let surface = match resolve_slot_mut(&mut state, slot_index, slot_generation) {
        Ok(surface) => surface,
        Err(code) => return code,
    };
    let record = match surface.submissions.get(&sequence) {
        Some(record) => record,
        // Retired already (or never issued for this surface).
        None => return renderer_status::ERR_STALE_HANDLE,
    };
    match record.state {
        SubmissionState::Pending => {
            // Acceptance is not completion: refuse.
            return renderer_status::ERR_PENDING;
        }
        SubmissionState::Completed => {}
        SubmissionState::Failed { .. } => {
            // Quarantine semantics: retirement of a failed submission is
            // only allowed on CONFIRMED device loss (the documented
            // release path); otherwise the record stays tracked.
            let Some(reason) = device_probe.as_ref().and_then(device_removed_reason) else {
                return renderer_status::ERR_QUARANTINE;
            };
            surface.submissions.remove(&sequence);
            unsafe {
                *out = GpuiGoRetireRecord {
                    terminal: retire_terminal::ABORTED_LOSS,
                    device_removed_reason: reason,
                    reserved: [0; 2],
                    record_size: std::mem::size_of::<GpuiGoRetireRecord>() as u32,
                };
            }
            return renderer_status::OK;
        }
    }
    // Completed: the exactly-once terminal record. Removing the entry
    // makes every later use of the handle stale.
    surface.submissions.remove(&sequence);
    unsafe {
        *out = GpuiGoRetireRecord {
            terminal: retire_terminal::COMPLETED,
            device_removed_reason: 0,
            reserved: [0; 2],
            record_size: std::mem::size_of::<GpuiGoRetireRecord>() as u32,
        };
    }
    renderer_status::OK
}

fn surface_destroy_body(surface: u64) -> i32 {
    let (slot_index, slot_generation) = surface_handle_parts(surface);
    let mut state = lock_global();
    // Reject while un-retired submissions remain (the contract's close
    // path: reject new work, drain, then detach).
    {
        let slot = &state.slots;
        if slot_index >= slot.len() {
            return renderer_status::ERR_BAD_HANDLE;
        }
        let surface = match resolve_slot(&state, slot_index, slot_generation) {
            Ok(surface) => surface,
            Err(code) => return code,
        };
        if !surface.submissions.is_empty() {
            return renderer_status::ERR_PENDING;
        }
    }
    // Unbind before dropping the swap chain (the pin's teardown order).
    if let Some(device) = state.device.as_ref() {
        let context = device.context.clone();
        unsafe { context.OMSetRenderTargets(None, None) };
    }
    let slot = &mut state.slots[slot_index];
    drop(slot.surface.take());
    slot.occupied = false;
    // slot_generation persists so destroyed handles stay stale after reuse.
    renderer_status::OK
}

fn device_info_body(
    out: *mut GpuiGoDeviceInfoRecord,
    buf: *mut u8,
    buf_len: u32,
    out_needed: *mut u32,
) -> i32 {
    if out.is_null() || out_needed.is_null() {
        return renderer_status::ERR_NULL_ARG;
    }
    if buf_len > 0 && buf.is_null() {
        return renderer_status::ERR_NULL_ARG;
    }
    let mut state = lock_global();
    let device = match ensure_device(&mut state) {
        Ok(device) => device,
        Err(code) => return code,
    };
    let facts = state
        .device
        .as_ref()
        .map(|device| &device.facts)
        .expect("ensure_device inserted the shared device");
    let buf: &mut [u8] = if buf_len == 0 {
        &mut []
    } else {
        unsafe { std::slice::from_raw_parts_mut(buf, buf_len as usize) }
    };
    let (record, needed) = pack_device_info(facts, buf);
    match record {
        Some(record) => {
            unsafe {
                *out = record;
                *out_needed = needed;
            }
            let _ = device;
            renderer_status::OK
        }
        None => {
            unsafe { *out_needed = needed };
            renderer_status::ERR_CAPACITY
        }
    }
}

// ---------------------------------------------------------------------------
// Table entries (panic boundary wrappers)
// ---------------------------------------------------------------------------

unsafe extern "system" fn renderer_surface_create(
    hwnd: u64,
    mode: u32,
    width: u32,
    height: u32,
    scale_bits: u32,
    out_surface: *mut u64,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| {
        surface_create_body(hwnd, mode, width, height, scale_bits, out_surface)
    }));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            renderer_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn renderer_surface_resize(
    surface: u64,
    width: u32,
    height: u32,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| surface_resize_body(surface, width, height)));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            renderer_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn renderer_surface_info(
    surface: u64,
    out: *mut GpuiGoSurfaceInfoRecord,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| surface_info_body(surface, out)));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            renderer_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn renderer_surface_present(
    surface: u64,
    rgba_bits: *const u32,
    out_submission: *mut u64,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| {
        surface_present_body(surface, rgba_bits, out_submission)
    }));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            renderer_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn renderer_surface_destroy(surface: u64) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| surface_destroy_body(surface)));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            renderer_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn renderer_submission_poll(
    submission: u64,
    out: *mut GpuiGoSubmissionStateRecord,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| submission_poll_body(submission, out)));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            renderer_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn renderer_submission_retire(
    submission: u64,
    out: *mut GpuiGoRetireRecord,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| submission_retire_body(submission, out)));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            renderer_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn renderer_device_info(
    out: *mut GpuiGoDeviceInfoRecord,
    buf: *mut u8,
    buf_len: u32,
    out_needed: *mut u32,
) -> i32 {
    let outcome =
        catch_unwind(AssertUnwindSafe(|| device_info_body(out, buf, buf_len, out_needed)));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            renderer_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn renderer_surface_draw_scene(
    surface: u64,
    scene: u64,
    appearance: u32,
    atlas: u64,
    out_submission: *mut u64,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| {
        draw::surface_draw_scene_body(surface, scene, appearance, atlas, out_submission)
    }));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            renderer_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn renderer_surface_render_scene(
    surface: u64,
    scene: u64,
    appearance: u32,
    atlas: u64,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| {
        draw::surface_render_scene_body(surface, scene, appearance, atlas)
    }));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            renderer_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn renderer_surface_read_pixels(
    surface: u64,
    out_bytes: *mut u8,
    capacity: u32,
    out_needed: *mut u32,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| {
        draw::surface_read_pixels_body(surface, out_bytes, capacity, out_needed)
    }));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            renderer_status::ERR_PANIC
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn table_self_check_fields_match_memory_truth() {
        assert_eq!(RENDERER_TABLE.service_version, GPUI_GO_RENDERER_SERVICE_VERSION);
        assert_eq!(
            RENDERER_TABLE.size_of_table as usize,
            std::mem::size_of::<GpuiGoRendererTable>()
        );
        assert_eq!(
            RENDERER_TABLE.align_of_table as usize,
            std::mem::align_of::<GpuiGoRendererTable>()
        );
        assert_eq!(
            RENDERER_TABLE.size_of_surface_info as usize,
            std::mem::size_of::<GpuiGoSurfaceInfoRecord>()
        );
        assert_eq!(
            RENDERER_TABLE.size_of_submission_state as usize,
            std::mem::size_of::<GpuiGoSubmissionStateRecord>()
        );
        assert_eq!(
            RENDERER_TABLE.size_of_retire_record as usize,
            std::mem::size_of::<GpuiGoRetireRecord>()
        );
        assert_eq!(
            RENDERER_TABLE.size_of_device_info as usize,
            std::mem::size_of::<GpuiGoDeviceInfoRecord>()
        );
        assert_eq!(RENDERER_TABLE.max_pending_submissions, MAX_PENDING_SUBMISSIONS);
    }

    #[test]
    fn every_table_slot_is_populated() {
        let slots = [
            RENDERER_TABLE.surface_create.map(|f| f as usize).unwrap_or(0),
            RENDERER_TABLE.surface_resize.map(|f| f as usize).unwrap_or(0),
            RENDERER_TABLE.surface_info.map(|f| f as usize).unwrap_or(0),
            RENDERER_TABLE.surface_present.map(|f| f as usize).unwrap_or(0),
            RENDERER_TABLE.surface_destroy.map(|f| f as usize).unwrap_or(0),
            RENDERER_TABLE.submission_poll.map(|f| f as usize).unwrap_or(0),
            RENDERER_TABLE.submission_retire.map(|f| f as usize).unwrap_or(0),
            RENDERER_TABLE.device_info.map(|f| f as usize).unwrap_or(0),
        ];
        assert!(slots.iter().all(|slot| *slot != 0));
    }

    #[test]
    fn pinned_swap_chain_constants() {
        // directx_renderer.rs: RENDER_TARGET_FORMAT and BUFFER_COUNT.
        assert_eq!(RENDER_TARGET_FORMAT_VALUE, DXGI_FORMAT_B8G8R8A8_UNORM.0 as u32);
        assert_eq!(RENDERER_BUFFER_COUNT, 3);
        // directx_renderer.rs: the two modes' alpha handling.
        assert_eq!(DXGI_ALPHA_MODE_PREMULTIPLIED.0, 1);
        assert_eq!(DXGI_ALPHA_MODE_IGNORE.0, 3);
        // directx_devices.rs: the feature-level order is 11.1 then 11.0.
        assert_eq!(FEATURE_LEVELS, [D3D_FEATURE_LEVEL_11_1, D3D_FEATURE_LEVEL_11_0]);
    }

    #[test]
    fn status_codes_are_distinct() {
        let codes = [
            renderer_status::OK,
            renderer_status::ERR_STALE_HANDLE,
            renderer_status::ERR_BAD_HANDLE,
            renderer_status::ERR_NULL_ARG,
            renderer_status::ERR_BAD_VALUE,
            renderer_status::ERR_DEVICE,
            renderer_status::ERR_BUSY,
            renderer_status::ERR_PENDING,
            renderer_status::ERR_QUARANTINE,
            renderer_status::ERR_PRESENT,
            renderer_status::ERR_CAPACITY,
            renderer_status::ERR_PANIC,
        ];
        for (i, code) in codes.iter().enumerate() {
            for other in codes.iter().skip(i + 1) {
                assert_ne!(code, other, "renderer status codes collide");
            }
        }
    }

    #[test]
    fn surface_handle_round_trip() {
        for (slot, generation) in [(0usize, 1u32), (3, 7), (0x1234, 0x000F_FFFF)] {
            let handle = make_surface_handle(slot, generation);
            assert_eq!(surface_handle_parts(handle), (slot, generation));
        }
    }

    #[test]
    fn submission_handle_round_trip() {
        for (slot, generation, sequence) in
            [(0usize, 1u32, 0u64), (5, 9, 0xFFFF_FFFF), (0xFFF, 0x000F_FFFF, 12345)]
        {
            let handle = make_submission_handle(slot, generation, sequence);
            assert_eq!(submission_handle_parts(handle), (slot, generation, sequence));
        }
    }

    #[test]
    fn surface_create_validates_arguments_before_device_use() {
        // A null out pointer is rejected before any D3D11 call.
        let code = surface_create_body(0x1234, 0, 100, 100, 0, core::ptr::null_mut());
        assert_eq!(code, renderer_status::ERR_NULL_ARG);
        // An unknown mode is a caller error, also before device creation
        // (the shared device is only created on first valid use).
        let mut handle = 0u64;
        let code = surface_create_body(0x1234, 2, 100, 100, 0, &mut handle);
        assert_eq!(code, renderer_status::ERR_BAD_VALUE);
        assert_eq!(handle, 0);
        // Oversized extents are caller errors.
        let code = surface_create_body(0x1234, 0, 2_000_000, 100, 0, &mut handle);
        assert_eq!(code, renderer_status::ERR_BAD_VALUE);
    }

    #[test]
    fn surface_and_submission_bodies_reject_bad_handles_and_args() {
        // Null out-records first.
        assert_eq!(surface_info_body(0, core::ptr::null_mut()), renderer_status::ERR_NULL_ARG);
        assert_eq!(submission_poll_body(0, core::ptr::null_mut()), renderer_status::ERR_NULL_ARG);
        assert_eq!(submission_retire_body(0, core::ptr::null_mut()), renderer_status::ERR_NULL_ARG);
        assert_eq!(
            surface_present_body(0, core::ptr::null(), core::ptr::null_mut()),
            renderer_status::ERR_NULL_ARG
        );
        // Malformed: slot beyond the (empty) registry.
        assert_eq!(
            surface_info_body(0xFFFF_FFFF_0000_0001, &mut blank_surface_info()),
            renderer_status::ERR_BAD_HANDLE
        );
        assert_eq!(surface_destroy_body(0xFFFF_FFFF_0000_0001), renderer_status::ERR_BAD_HANDLE);
        // Never-issued surface slot.
        assert_eq!(
            surface_info_body(make_surface_handle(0, 1), &mut blank_surface_info()),
            renderer_status::ERR_BAD_HANDLE
        );
        // Never-issued submission sequence.
        assert_eq!(
            submission_poll_body(make_submission_handle(0, 1, 0), &mut blank_submission_state()),
            renderer_status::ERR_BAD_HANDLE
        );
        assert_eq!(
            submission_retire_body(make_submission_handle(0, 1, 0), &mut blank_retire_record()),
            renderer_status::ERR_BAD_HANDLE
        );
    }

    #[test]
    fn device_info_packs_strings_and_reports_capacity() {
        let facts = DeviceFacts {
            vendor_id: 0x10DE,
            device_id: 0x1234,
            software: false,
            dedicated_video_memory: 8_589_934_592,
            feature_level: D3D_FEATURE_LEVEL_11_1.0 as u32,
            adapter_desc: "Test Adapter".to_string(),
            driver_name: "NVIDIA Corporation".to_string(),
            driver_version: "580.88 test".to_string(),
            multithread_protected: true,
        };
        let mut big = [0u8; 256];
        let (record, needed) = pack_device_info(&facts, &mut big);
        let record = record.expect("256 bytes is plenty");
        assert_eq!(record.vendor_id, 0x10DE);
        assert_eq!(record.feature_level, 0xb100);
        assert_eq!(record.record_size, std::mem::size_of::<GpuiGoDeviceInfoRecord>() as u32);
        assert_eq!(record.reserved, 0);
        let take = |off: u32, len: u32| -> String {
            String::from_utf8(big[off as usize..(off + len) as usize].to_vec()).expect("utf8")
        };
        assert_eq!(take(record.adapter_desc_offset, record.adapter_desc_len), "Test Adapter");
        assert_eq!(take(record.driver_name_offset, record.driver_name_len), "NVIDIA Corporation");
        assert_eq!(take(record.driver_version_offset, record.driver_version_len), "580.88 test");
        let needed_bytes =
            "Test Adapter".len() + "NVIDIA Corporation".len() + "580.88 test".len();
        assert_eq!(needed as usize, needed_bytes);

        // Too-small buffer: the record is withheld and the need returned.
        let mut small = [0u8; 4];
        let (record, needed) = pack_device_info(&facts, &mut small);
        assert!(record.is_none());
        assert_eq!(needed as usize, needed_bytes);
        // Zero-capacity call also reports the need.
        let (record, needed) = pack_device_info(&facts, &mut []);
        assert!(record.is_none());
        assert!(needed > 0);
    }

    #[test]
    fn vendor_id_driver_name_mapping_matches_the_pin() {
        assert_eq!(driver_name_for(0x10DE), "NVIDIA Corporation");
        assert_eq!(driver_name_for(0x1002), "AMD Corporation");
        assert_eq!(driver_name_for(0x8086), "Intel Corporation");
        assert_eq!(driver_name_for(0x1234), "Unknown Vendor (ID: 0x1234)");
    }

    #[test]
    fn surface_info_record_shape() {
        let record = blank_surface_info();
        // The compile-time pin above asserts 36/4; size_of_val double-checks
        // the same fact at runtime (the blank's record_size self-check is
        // filled by the native writer, not the test).
        assert_eq!(std::mem::size_of_val(&record), 36);
        assert_eq!(std::mem::align_of_val(&record), 4);
    }

    fn blank_surface_info() -> GpuiGoSurfaceInfoRecord {
        GpuiGoSurfaceInfoRecord {
            mode: 0,
            format: 0,
            buffer_count: 0,
            width: 0,
            height: 0,
            feature_level: 0,
            resize_generation: 0,
            scale_bits: 0,
            record_size: 0,
        }
    }

    fn blank_submission_state() -> GpuiGoSubmissionStateRecord {
        GpuiGoSubmissionStateRecord {
            state: u32::MAX,
            device_removed_reason: -1,
            data_hr: -1,
            present_hr: -1,
            record_size: 0,
        }
    }

    fn blank_retire_record() -> GpuiGoRetireRecord {
        GpuiGoRetireRecord { terminal: u32::MAX, device_removed_reason: -1, reserved: [0; 2], record_size: 0 }
    }
}
