//! gpui-go native capture service (ticket28, reserved slot 10).
//!
//! The pinned Windows Graphics Capture producer surface of the gpui-CE
//! reference, driven through the SAME `windows` 0.62.2 crate the
//! renderer service already uses (its WinRT bindings; NO
//! windows-capture crate and no second windows version — the pin uses
//! the `windows-capture` crate wrapper, and this port uses the
//! underlying WinRT APIs directly, which is the smaller and
//! version-consistent dependency decision; the observable capture
//! semantics are the pinned ones):
//!
//! * **Source discovery** — the pin's `screen_sources`
//!   (platform/screen_capture/windows.rs): monitor enumeration with
//!   resolution, label, is-main and a 1-based id. EnumDisplayMonitors +
//!   GetMonitorInfoW (the friendly device name; the primary flag
//!   replaces the pin's primary-monitor comparison — equivalent
//!   observable metadata).
//! * **The WinRT producer on the ACTUAL renderer device** — the
//!   ticket's first criterion: `IDXGIDevice` cast from the renderer
//!   service's shared D3D11 device, `CreateDirect3D11DeviceFromDXGIDevice`
//!   → `IDirect3DDevice`, `Direct3D11CaptureFramePool::CreateFreeThreaded`
//!   (BGRA, 2 buffers, the item size), `CreateCaptureSession(item)`,
//!   `StartCapture` — the same-device frame pool whose textures the
//!   renderer service's surface draw path can open directly (the pin's
//!   draw_surfaces comment: same-device capture textures).
//! * **Free-threaded frame-arrival notifications** — the `FrameArrived`
//!   event of the free-threaded pool fires on a Windows thread pool
//!   thread; the handler ONLY enqueues (raw texture + content size +
//!   timestamp) into the stream's bounded pending queue (the pin's
//!   on_frame_arrived extracts the raw texture and hands it to the app
//!   callback — the queue is the ABI-safe equivalent of the callback).
//! * **Foreground checkout/copy** — `stream_poll` runs on the caller's
//!   (foreground) thread: it takes the renderer service's global
//!   device lock (the specified immediate-context protection; the
//!   device is multithread-protected), copies each pending frame into
//!   an immutable destination texture (`D3D11_USAGE_DEFAULT`,
//!   `BIND_SHADER_RESOURCE`, written exactly once by this copy), then
//!   releases the raw capture frame — "retain the actual capture frame
//!   through copy completion, then retain immutable destination storage
//!   through later draws; pool reuse never mutates cached content"
//!   (the raw frame-pool buffers are recycled regardless of COM
//!   refcounts, so the copy is what makes published content immutable).
//! * **Bounded pending frames** — the queue holds at most
//!   [`MAX_PENDING_CAPTURE_FRAMES`]; overflow drops the OLDEST
//!   un-copied frames (keeping the newest, live-capture semantics)
//!   and counts them in the stream stats.
//! * **ContentSize/crop/resize** — each frame record carries the
//!   capture item's current content size next to the pool texture's
//!   size (the destination is pool-sized; `CopyResource` requires
//!   equal dimensions — the content size is the caller's crop). A
//!   content-size change from the pool size triggers
//!   `frame_pool.Recreate` (the WGC size-change protocol) and is
//!   counted.
//! * **Stop/close** — `stream_stop` stops the capture session (the
//!   pin's `CaptureStream::drop` → `capture.stop()`); late frame
//!   arrivals are refused by the stopped flag.
//!   `stream_destroy` drops the session, pool and item on the MTA
//!   housekeeping thread (apartment-affine release kept on the
//!   apartment that created the objects) and releases every published
//!   frame texture.
//!
//! All WinRT object creation and destruction runs on ONE lazily
//! created native MTA housekeeping thread (RoInitialize
//! MULTITHREADED); the Go caller's thread never touches WinRT
//! activation (the Go host's foreground thread is OLE STA and must
//! not have its apartment changed). D3D11 COM objects (the device,
//! textures) are free-threaded under multithread protection and are
//! used from the caller's thread under the renderer service's global
//! lock.
//!
//! ABI shape (the established conventions): fixed-width
//! `#[repr(C)]` records, caller-owned buffers, every export
//! containing panics with `catch_unwind`, and a stream slot registry
//! (handle = `(slot index << 20) | slot generation`, stale after
//! destroy or slot reuse, capacity [`MAX_CAPTURE_STREAMS`]).

use std::collections::{HashMap, VecDeque};
use std::panic::{catch_unwind, AssertUnwindSafe};
use std::sync::atomic::{AtomicBool, AtomicU32, Ordering};
use std::sync::mpsc::{channel, Sender};
use std::sync::{Arc, Mutex, MutexGuard, OnceLock};
use std::thread::JoinHandle;

use windows::core::{BOOL, Interface};
use windows::Win32::Foundation::LPARAM;
use windows::Win32::Graphics::Gdi::{EnumDisplayMonitors, GetMonitorInfoW, HMONITOR, MONITORINFO, MONITORINFOEXW};
use windows::Win32::UI::WindowsAndMessaging::MONITORINFOF_PRIMARY;
use windows::Win32::Graphics::Direct3D11::{
    ID3D11Device, ID3D11Texture2D, D3D11_BIND_SHADER_RESOURCE, D3D11_TEXTURE2D_DESC,
    D3D11_USAGE_DEFAULT,
};
use windows::Win32::Graphics::Dxgi::Common::{DXGI_FORMAT_B8G8R8A8_UNORM, DXGI_SAMPLE_DESC};
use windows::Win32::Graphics::Dxgi::IDXGIDevice;
use windows::Win32::System::WinRT::Direct3D11::{CreateDirect3D11DeviceFromDXGIDevice, IDirect3DDxgiInterfaceAccess};
use windows::Win32::System::WinRT::{RoInitialize, RO_INIT_MULTITHREADED};

// ---------------------------------------------------------------------------
// Constants and status codes
// ---------------------------------------------------------------------------

/// Live capture stream slot bound.
pub const MAX_CAPTURE_STREAMS: u32 = 16;

/// Bounded pending-frame queue per stream (the ticket's "bounded
/// pending frames"; overflow drops the oldest).
pub const MAX_PENDING_CAPTURE_FRAMES: u32 = 8;

/// The frame pool buffer count (the pin's windows-capture Settings
/// default is 2 buffers).
pub const CAPTURE_BUFFER_COUNT: i32 = 2;

/// Capture service status codes. 0 is success; negative values are
/// caller/argument errors; 100+ are internal failures.
pub mod capture_status {
    /// Success.
    pub const OK: i32 = 0;
    /// The stream/frame handle encoded a past generation (destroyed or
    /// slot reused since).
    pub const ERR_STALE_HANDLE: i32 = -1;
    /// The handle never existed or is malformed.
    pub const ERR_BAD_HANDLE: i32 = -2;
    /// A required pointer argument was null.
    pub const ERR_NULL_ARG: i32 = -3;
    /// An invalid count or record-size value.
    pub const ERR_BAD_VALUE: i32 = -4;
    /// Windows Graphics Capture is unavailable on this system (the
    /// item interop refused the monitor — blocked environment, not a
    /// passed capture behavior).
    pub const ERR_UNSUPPORTED: i32 = -5;
    /// The stream is stopped (poll on a stopped stream drains nothing
    /// new; frames already pending remain drainable).
    pub const ERR_STOPPED: i32 = -6;
    /// The capture device could not be created from the renderer's
    /// D3D11 device.
    pub const ERR_DEVICE: i32 = -7;
    /// The frame's texture could not be copied into destination
    /// storage (the HRESULT is carried in the stream stats).
    pub const ERR_COPY: i32 = -8;
    /// A record-size self-check mismatched this service's layout.
    pub const ERR_RECORD_SIZE: i32 = -9;
    /// The poll's frame-record buffer was too small (retry; nothing
    /// was drained).
    pub const ERR_CAPACITY: i32 = -10;
    /// A panic was contained by `catch_unwind` before returning.
    pub const ERR_PANIC: i32 = 100;
}

// ---------------------------------------------------------------------------
// ABI records
// ---------------------------------------------------------------------------

/// One enumerated capture source (the pin's `SourceMetadata`:
/// resolution, label, is_main, 1-based id). Layout: `hmonitor` @0
/// (u64), `width` @8, `height` @12, `is_main` @16, `id` @20,
/// `label_offset` @24, `label_len` @28, `record_size` @32; size 36,
/// alignment 4.
#[repr(C)]
#[derive(Clone, Copy)]
pub struct GpuiGoCaptureSourceRecord {
    /// The monitor handle (stable for the enumeration; the stream is
    /// created from it).
    pub hmonitor: u64,
    /// The monitor resolution width in device pixels.
    pub width: i32,
    /// The monitor resolution height.
    pub height: i32,
    /// 1 when this is the primary monitor (the pin's is_main).
    pub is_main: u32,
    /// The 1-based source id (the pin's id = index + 1).
    pub id: u32,
    /// Byte offset of the label in the string buffer.
    pub label_offset: u32,
    /// Label byte length.
    pub label_len: u32,
    /// Self-check: `size_of::<GpuiGoCaptureSourceRecord>()`.
    pub record_size: u32,
}

impl Default for GpuiGoCaptureSourceRecord {
    fn default() -> Self {
        GpuiGoCaptureSourceRecord {
            hmonitor: 0,
            width: 0,
            height: 0,
            is_main: 0,
            id: 0,
            label_offset: 0,
            label_len: 0,
            record_size: core::mem::size_of::<GpuiGoCaptureSourceRecord>() as u32,
        }
    }
}

const _: () = assert!(core::mem::size_of::<GpuiGoCaptureSourceRecord>() == 40);
const _: () = assert!(core::mem::align_of::<GpuiGoCaptureSourceRecord>() == 8);

/// One published capture frame (the immutable copy plus its content
/// size). Layout: `handle` @0 (u64), `width` @8, `height` @12,
/// `content_width` @16, `content_height` @20, `timestamp` @24 (u64,
/// 100ns units), `record_size` @32; size 40, alignment 8.
#[repr(C)]
pub struct GpuiGoCaptureFrameRecord {
    /// The frame handle (release it with `frame_release`).
    pub handle: u64,
    /// The destination texture's width (the pool buffer size).
    pub width: u32,
    /// The destination texture's height.
    pub height: u32,
    /// The capture item's content width at capture time (the crop).
    pub content_width: i32,
    /// The capture item's content height.
    pub content_height: i32,
    /// The frame timestamp (SystemRelativeTime Duration clamped at 0).
    pub timestamp: u64,
    /// Self-check: `size_of::<GpuiGoCaptureFrameRecord>()`.
    pub record_size: u32,
}

impl Default for GpuiGoCaptureFrameRecord {
    fn default() -> Self {
        GpuiGoCaptureFrameRecord {
            handle: 0,
            width: 0,
            height: 0,
            content_width: 0,
            content_height: 0,
            timestamp: 0,
            record_size: core::mem::size_of::<GpuiGoCaptureFrameRecord>() as u32,
        }
    }
}

const _: () = assert!(core::mem::size_of::<GpuiGoCaptureFrameRecord>() == 40);
const _: () = assert!(core::mem::align_of::<GpuiGoCaptureFrameRecord>() == 8);

/// Stream stats record (observability: the bounded queue, the drop
/// counter, the size-change counter). Layout: `pending` @0,
/// `published` @4, `dropped` @8, `size_changes` @12, `stopped` @16,
/// `pool_width` @20, `pool_height` @24, `last_error_hr` @28 (i32),
/// `record_size` @32; size 36, alignment 4.
#[repr(C)]
pub struct GpuiGoCaptureStreamStats {
    /// Undrained pending frames right now.
    pub pending: u32,
    /// Published (copied) frames not yet released.
    pub published: u32,
    /// Frames dropped from the bounded queue (drop-oldest overflow).
    pub dropped: u32,
    /// Frame pool Recreate calls (content-size changes).
    pub size_changes: u32,
    /// 1 when the session is stopped.
    pub stopped: u32,
    /// The current frame pool width.
    pub pool_width: i32,
    /// The current frame pool height.
    pub pool_height: i32,
    /// The last HRESULT that failed a copy or resource call (0 = none).
    pub last_error_hr: i32,
    /// Self-check: `size_of::<GpuiGoCaptureStreamStats>()`.
    pub record_size: u32,
}

impl Default for GpuiGoCaptureStreamStats {
    fn default() -> Self {
        GpuiGoCaptureStreamStats {
            pending: 0,
            published: 0,
            dropped: 0,
            size_changes: 0,
            stopped: 0,
            pool_width: 0,
            pool_height: 0,
            last_error_hr: 0,
            record_size: core::mem::size_of::<GpuiGoCaptureStreamStats>() as u32,
        }
    }
}

const _: () = assert!(core::mem::size_of::<GpuiGoCaptureStreamStats>() == 36);
const _: () = assert!(core::mem::align_of::<GpuiGoCaptureStreamStats>() == 4);

/// The capture service table (reserved slot 10 of the bootstrap
/// table).
#[repr(C)]
pub struct GpuiGoCaptureTable {
    /// `capture_supported` — is Windows Graphics Capture available.
    pub supported: Option<unsafe extern "system" fn(out_supported: *mut u32) -> i32>,
    /// `capture_sources_enumerate`.
    pub sources_enumerate: Option<
        unsafe extern "system" fn(
            out_records: *mut GpuiGoCaptureSourceRecord,
            record_capacity: u32,
            strings_buf: *mut u8,
            strings_capacity: u32,
            out_count: *mut u32,
            out_strings_needed: *mut u32,
        ) -> i32,
    >,
    /// `capture_stream_create` (the WinRT producer on the renderer's
    /// device; the initial item size is returned).
    pub stream_create: Option<
        unsafe extern "system" fn(
            hmonitor: u64,
            out_stream: *mut u64,
            out_width: *mut i32,
            out_height: *mut i32,
        ) -> i32,
    >,
    /// `capture_stream_poll` (foreground checkout/copy).
    pub stream_poll: Option<
        unsafe extern "system" fn(
            stream: u64,
            out_frames: *mut GpuiGoCaptureFrameRecord,
            frame_capacity: u32,
            out_count: *mut u32,
        ) -> i32,
    >,
    /// `capture_stream_stop`.
    pub stream_stop: Option<unsafe extern "system" fn(stream: u64) -> i32>,
    /// `capture_stream_destroy`.
    pub stream_destroy: Option<unsafe extern "system" fn(stream: u64) -> i32>,
    /// `capture_stream_stats`.
    pub stream_stats: Option<
        unsafe extern "system" fn(stream: u64, out_stats: *mut GpuiGoCaptureStreamStats) -> i32,
    >,
    /// `capture_frame_release`.
    pub frame_release: Option<unsafe extern "system" fn(frame: u64) -> i32>,
    /// `capture_panic_probe` (test-only; panics inside catch_unwind).
    pub panic_probe: Option<unsafe extern "system" fn() -> i32>,
    /// Capture ABI version.
    pub service_version: u32,
    /// `size_of::<GpuiGoCaptureTable>()` self-check.
    pub size_of_table: u32,
    /// `align_of::<GpuiGoCaptureTable>()` self-check.
    pub align_of_table: u32,
    /// `size_of::<GpuiGoCaptureSourceRecord>()` self-check.
    pub size_of_source_record: u32,
    /// `size_of::<GpuiGoCaptureFrameRecord>()` self-check.
    pub size_of_frame_record: u32,
    /// `size_of::<GpuiGoCaptureStreamStats>()` self-check.
    pub size_of_stream_stats: u32,
    /// [`MAX_CAPTURE_STREAMS`].
    pub max_streams: u32,
    /// [`MAX_PENDING_CAPTURE_FRAMES`].
    pub max_pending_frames: u32,
    /// [`CAPTURE_BUFFER_COUNT`].
    pub buffer_count: u32,
}

/// Capture service ABI version.
pub const GPUI_GO_CAPTURE_SERVICE_VERSION: u32 = 1;

pub(crate) static CAPTURE_TABLE: GpuiGoCaptureTable = GpuiGoCaptureTable {
    supported: Some(capture_supported),
    sources_enumerate: Some(capture_sources_enumerate),
    stream_create: Some(capture_stream_create),
    stream_poll: Some(capture_stream_poll),
    stream_stop: Some(capture_stream_stop),
    stream_destroy: Some(capture_stream_destroy),
    stream_stats: Some(capture_stream_stats),
    frame_release: Some(capture_frame_release),
    panic_probe: Some(capture_panic_probe),
    service_version: GPUI_GO_CAPTURE_SERVICE_VERSION,
    size_of_table: core::mem::size_of::<GpuiGoCaptureTable>() as u32,
    align_of_table: core::mem::align_of::<GpuiGoCaptureTable>() as u32,
    size_of_source_record: core::mem::size_of::<GpuiGoCaptureSourceRecord>() as u32,
    size_of_frame_record: core::mem::size_of::<GpuiGoCaptureFrameRecord>() as u32,
    size_of_stream_stats: core::mem::size_of::<GpuiGoCaptureStreamStats>() as u32,
    max_streams: MAX_CAPTURE_STREAMS,
    max_pending_frames: MAX_PENDING_CAPTURE_FRAMES,
    buffer_count: CAPTURE_BUFFER_COUNT as u32,
};

const _: () = assert!(core::mem::size_of::<GpuiGoCaptureTable>() == 112);
const _: () = assert!(core::mem::align_of::<GpuiGoCaptureTable>() == 8);

// ---------------------------------------------------------------------------
// WinRT imports (behind the feature set; see Cargo.toml)
// ---------------------------------------------------------------------------

use windows::Graphics::Capture::{
    Direct3D11CaptureFrame, Direct3D11CaptureFramePool, GraphicsCaptureItem,
    GraphicsCaptureSession,
};
use windows::Graphics::DirectX::Direct3D11::IDirect3DDevice;
use windows::Graphics::DirectX::DirectXPixelFormat;
use windows::Win32::System::WinRT::Graphics::Capture::IGraphicsCaptureItemInterop;

// ---------------------------------------------------------------------------
// Internal state
// ---------------------------------------------------------------------------

/// One pending (un-copied) frame: the raw frame-pool texture clone —
/// the "actual capture frame" retained until the copy completes. The
/// pool recycles buffer CONTENT regardless of COM refcounts, so this
/// is mutable until the poll's copy.
struct PendingFrame {
    texture: ID3D11Texture2D,
    width: u32,
    height: u32,
    content_width: i32,
    content_height: i32,
    timestamp: u64,
}

/// One published frame: the immutable destination texture (written
/// exactly once by the poll's copy) and its pool dimensions (the
/// content size and timestamp live in the frame record handed to the
/// caller; the draw path needs only the texture).
struct PublishedFrame {
    texture: ID3D11Texture2D,
    width: u32,
    height: u32,
}

/// The state shared with the native FrameArrived handler (runs on a
/// Windows thread pool thread).
struct StreamShared {
    pending: Mutex<VecDeque<PendingFrame>>,
    stopped: AtomicBool,
    dropped: AtomicU32,
}

/// One capture stream.
struct CaptureStream {
    /// The WinRT producer objects. Dropped on the MTA housekeeping
    /// thread at destroy (apartment-affine release discipline).
    item: Option<GraphicsCaptureItem>,
    pool: Option<Direct3D11CaptureFramePool>,
    session: Option<GraphicsCaptureSession>,
    shared: Arc<StreamShared>,
    /// Published frames by frame handle.
    published: HashMap<u64, PublishedFrame>,
    next_frame_seq: u64,
    pool_width: i32,
    pool_height: i32,
    size_changes: u32,
    last_error_hr: i32,
}

struct CaptureSlot {
    slot_generation: u32,
    occupied: bool,
    stream: Option<CaptureStream>,
}

struct CaptureServiceState {
    slots: Vec<CaptureSlot>,
}

static GLOBAL: Mutex<CaptureServiceState> =
    Mutex::new(CaptureServiceState { slots: Vec::new() });

// SAFETY: the state holds WinRT objects (conservatively !Send) and
// D3D11 COM objects. The WinRT objects are only created, recreated
// and dropped on the MTA housekeeping thread (stream create/stop/
// destroy route through it; poll only touches the D3D textures under
// the renderer lock, and the FrameArrived handler only touches the
// per-stream Arc queue). No object is ever used concurrently.
unsafe impl Send for CaptureServiceState {}

fn lock_global() -> MutexGuard<'static, CaptureServiceState> {
    GLOBAL.lock().unwrap_or_else(|poisoned| poisoned.into_inner())
}

/// Stream handle: `(slot index << 20) | slot generation`.
fn stream_handle_parts(handle: u64) -> (usize, u32) {
    ((handle >> 20) as usize, (handle & 0xFFFFF) as u32)
}

fn make_stream_handle(slot: usize, generation: u32) -> u64 {
    ((slot as u64) << 20) | (generation as u64 & 0xFFFFF)
}

/// Frame handle: `(slot index << 44) | (slot generation << 24) |
/// frame sequence (24 bits, wrapping)`.
fn make_frame_handle(slot: usize, generation: u32, seq: u64) -> u64 {
    ((slot as u64) << 44)
        | ((generation as u64 & 0xFFFFF) << 24)
        | (seq & 0xFFFFFF)
}

fn frame_handle_parts(handle: u64) -> (usize, u32, u64) {
    (
        (handle >> 44) as usize,
        ((handle >> 24) & 0xFFFFF) as u32,
        handle & 0xFFFFFF,
    )
}

// ---------------------------------------------------------------------------
// The MTA housekeeping thread
// ---------------------------------------------------------------------------

/// A command for the MTA housekeeping thread. The WinRT objects are
/// created and dropped there (apartment-affine release discipline).
/// A WinRT object moved across threads for MTA-affine create/drop.
/// The MOVE is safe (a COM interface pointer is a pointer); the USE
/// stays on the MTA housekeeping thread by construction — the wrapper
/// exists so the command channel can carry the objects.
struct MtaOwned<T>(T);

// SAFETY: the wrapped object is only dereferenced on the MTA
// housekeeping thread (the channel's receiver side); moving a COM
// pointer between threads is safe under that discipline.
unsafe impl<T> Send for MtaOwned<T> {}

impl<T> MtaOwned<T> {
    fn into_inner(self) -> T {
        self.0
    }
    fn from(value: T) -> Self {
        MtaOwned(value)
    }
}

enum Command {
    /// Probe Windows Graphics Capture availability (create an item for
    /// the first monitor).
    Supported { hmonitor: u64, reply: Sender<Result<bool, i32>> },
    /// Create the frame pool + session for a monitor on the given
    /// Direct3D device.
    CreateStream {
        hmonitor: u64,
        device: MtaOwned<IDirect3DDevice>,
        reply: Sender<
            Result<
                (
                    MtaOwned<GraphicsCaptureItem>,
                    MtaOwned<Direct3D11CaptureFramePool>,
                    MtaOwned<GraphicsCaptureSession>,
                    i32,
                    i32,
                ),
                i32,
            >,
        >,
    },
    /// Stop the session (the pin's capture.stop(); the WinRT session
    /// stops through its IClosable Close()).
    Stop {
        session: MtaOwned<GraphicsCaptureSession>,
        reply: Sender<Result<(), i32>>,
    },
    /// Drop the producer objects (apartment-affine release on the MTA
    /// thread that created them).
    Drop {
        item: Option<MtaOwned<GraphicsCaptureItem>>,
        pool: Option<MtaOwned<Direct3D11CaptureFramePool>>,
        session: Option<MtaOwned<GraphicsCaptureSession>>,
        reply: Sender<Result<(), i32>>,
    },
}

static COMMAND_TX: OnceLock<Sender<Command>> = OnceLock::new();

/// Returns the command channel to the housekeeping thread, starting
/// the thread on first use.
fn command_channel() -> &'static Sender<Command> {
    COMMAND_TX.get_or_init(|| {
        let (tx, rx) = channel::<Command>();
        let _handle: JoinHandle<()> = std::thread::Builder::new()
            .name("gpui-go-capture-mta".to_owned())
            .spawn(move || {
                // MTA initialization (RoInitialize multithreaded;
                // S_FALSE for a re-init on this thread is fine).
                let _ = unsafe { RoInitialize(RO_INIT_MULTITHREADED) };
                while let Ok(command) = rx.recv() {
                    match command {
                        Command::Supported { hmonitor, reply } => {
                            let _ = reply.send(supported_impl(hmonitor));
                        }
                        Command::CreateStream { hmonitor, device, reply } => {
                            let _ = reply.send(create_stream_impl(hmonitor, &device.into_inner()));
                        }
                        Command::Stop { session, reply } => {
                            // The WinRT session stops through IClosable
                            // (GraphicsCaptureSession::Close).
                            let session = session.into_inner();
                            let result = session.Close().map_err(|e| e.code().0).map(|_| ());
                            let _ = reply.send(result);
                        }
                        Command::Drop { item, pool, session, reply } => {
                            // Drop in reverse creation order; Release
                            // happens on this MTA thread.
                            drop(session.map(MtaOwned::into_inner));
                            drop(pool.map(MtaOwned::into_inner));
                            drop(item.map(MtaOwned::into_inner));
                            let _ = reply.send(Ok(()));
                        }
                    }
                }
            })
            .expect("capture housekeeping thread");
        tx
    })
}

/// Creates the GraphicsCaptureItem for a monitor (must run on an
/// apartment-initialized thread).
fn item_for_monitor(hmonitor: u64) -> Result<GraphicsCaptureItem, i32> {
    let interop: IGraphicsCaptureItemInterop =
        windows::core::factory::<GraphicsCaptureItem, IGraphicsCaptureItemInterop>()
            .map_err(|e| e.code().0)?;
    unsafe {
        interop
            .CreateForMonitor::<GraphicsCaptureItem>(windows::Win32::Graphics::Gdi::HMONITOR(
                hmonitor as *mut _,
            ))
            .map_err(|e| e.code().0)
    }
}

fn supported_impl(hmonitor: u64) -> Result<bool, i32> {
    item_for_monitor(hmonitor).map(|_| true)
}

fn create_stream_impl(
    hmonitor: u64,
    device: &IDirect3DDevice,
) -> Result<
    (
        MtaOwned<GraphicsCaptureItem>,
        MtaOwned<Direct3D11CaptureFramePool>,
        MtaOwned<GraphicsCaptureSession>,
        i32,
        i32,
    ),
    i32,
> {
    let item = item_for_monitor(hmonitor)?;
    let size = item.Size().map_err(|e| e.code().0)?;
    let pool = Direct3D11CaptureFramePool::CreateFreeThreaded(
        device,
        DirectXPixelFormat::B8G8R8A8UIntNormalized,
        CAPTURE_BUFFER_COUNT,
        size,
    )
    .map_err(|e| e.code().0)?;
    let session = pool
        .CreateCaptureSession(&item)
        .map_err(|e| e.code().0)?;
    session.StartCapture().map_err(|e| e.code().0)?;
    Ok((
        MtaOwned::from(item),
        MtaOwned::from(pool),
        MtaOwned::from(session),
        size.Width,
        size.Height,
    ))
}

// ---------------------------------------------------------------------------
// Monitor enumeration (Win32; the caller's thread)
// ---------------------------------------------------------------------------

struct EnumerateCtx {
    monitors: Vec<u64>,
}

unsafe extern "system" fn enum_monitors_proc(
    hmonitor: HMONITOR,
    _hdc: windows::Win32::Graphics::Gdi::HDC,
    _rect: *mut windows::Win32::Foundation::RECT,
    lparam: LPARAM,
) -> BOOL {
    // SAFETY: EnumDisplayMonitors invokes the callback synchronously
    // on the calling thread with the LPARAM we passed.
    let ctx = unsafe { &mut *(lparam.0 as *mut EnumerateCtx) };
    ctx.monitors.push(hmonitor.0 as u64);
    BOOL(1)
}

// ---------------------------------------------------------------------------
// Body implementations
// ---------------------------------------------------------------------------

fn supported_body(out_supported: *mut u32) -> i32 {
    unsafe {
    if out_supported.is_null() {
            return capture_status::ERR_NULL_ARG;
        }
        *out_supported = 0;
        // The primary monitor (or any monitor) resolving to a capture
        // item is the availability probe.
        let monitors = enumerate_monitor_handles();
        let Some(hmonitor) = monitors.first() else {
            return capture_status::ERR_UNSUPPORTED;
        };
        let tx = command_channel();
        let (reply_tx, reply_rx) = channel();
        if tx
            .send(Command::Supported { hmonitor: *hmonitor, reply: reply_tx })
            .is_err()
        {
            return capture_status::ERR_DEVICE;
        }
        match reply_rx.recv() {
            Ok(Ok(true)) => {
                *out_supported = 1;
                capture_status::OK
            }
            Ok(Ok(false)) => capture_status::ERR_UNSUPPORTED,
            Ok(Err(code)) => code,
            Err(_) => capture_status::ERR_DEVICE,
        }
    }
}

fn enumerate_monitor_handles() -> Vec<u64> {
    let mut ctx = EnumerateCtx { monitors: Vec::new() };
    unsafe {
        let _ = EnumDisplayMonitors(
            None,
            None,
            Some(enum_monitors_proc),
            LPARAM(&mut ctx as *mut _ as isize),
        );
    }
    ctx.monitors
}

fn sources_enumerate_body(
    out_records: *mut GpuiGoCaptureSourceRecord,
    record_capacity: u32,
    strings_buf: *mut u8,
    strings_capacity: u32,
    out_count: *mut u32,
    out_strings_needed: *mut u32,
) -> i32 {
    unsafe {
        if out_count.is_null() || out_strings_needed.is_null() {
            return capture_status::ERR_NULL_ARG;
        }
        *out_count = 0;
        *out_strings_needed = 0;
        let monitors = enumerate_monitor_handles();
        // Build the records with labels first (to compute the needed
        // string bytes).
        let mut labels: Vec<String> = Vec::with_capacity(monitors.len());
        let mut records: Vec<GpuiGoCaptureSourceRecord> = Vec::with_capacity(monitors.len());
        for (index, hmonitor) in monitors.iter().enumerate() {
            let mut info = MONITORINFOEXW::default();
            info.monitorInfo.cbSize = core::mem::size_of::<MONITORINFO>() as u32;
            let ok = GetMonitorInfoW(
                HMONITOR(*hmonitor as *mut _),
                &mut info.monitorInfo,
            );
            let is_main = u32::from(info.monitorInfo.dwFlags & MONITORINFOF_PRIMARY != 0);
            let label = if ok.as_bool() {
                let len = info.szDevice.iter().position(|c| *c == 0).unwrap_or(32);
                String::from_utf16_lossy(&info.szDevice[..len])
            } else {
                String::new()
            };
            let label_offset = labels.iter().map(|l| l.len()).sum::<usize>();
            labels.push(label.clone());
            records.push(GpuiGoCaptureSourceRecord {
                hmonitor: *hmonitor,
                width: info.monitorInfo.rcMonitor.right - info.monitorInfo.rcMonitor.left,
                height: info.monitorInfo.rcMonitor.bottom - info.monitorInfo.rcMonitor.top,
                is_main,
                id: index as u32 + 1,
                label_offset: label_offset as u32,
                label_len: label.len() as u32,
                record_size: core::mem::size_of::<GpuiGoCaptureSourceRecord>() as u32,
            });
        }
        let needed: usize = labels.iter().map(|l| l.len()).sum();
        if needed > strings_capacity as usize
            || (needed > 0 && strings_buf.is_null())
            || records.len() > record_capacity as usize
        {
            if records.len() > record_capacity as usize && record_capacity == 0 {
                // The caller is probing the count; report both.
                *out_count = records.len() as u32;
                *out_strings_needed = needed as u32;
                return capture_status::ERR_CAPACITY;
            }
            *out_count = records.len() as u32;
            *out_strings_needed = needed as u32;
            return capture_status::ERR_CAPACITY;
        }
        if record_capacity > 0 && out_records.is_null() {
            return capture_status::ERR_NULL_ARG;
        }
        let out = std::slice::from_raw_parts_mut(out_records, records.len());
        out.copy_from_slice(&records);
        let mut cursor = 0usize;
        for label in &labels {
            if !label.is_empty() {
                std::ptr::copy_nonoverlapping(
                    label.as_ptr(),
                    strings_buf.add(cursor),
                    label.len(),
                );
            }
            cursor += label.len();
        }
        *out_count = records.len() as u32;
        *out_strings_needed = needed as u32;
        capture_status::OK
    }
}

fn stream_create_body(
    hmonitor: u64,
    out_stream: *mut u64,
    out_width: *mut i32,
    out_height: *mut i32,
) -> i32 {
    unsafe {
        if out_stream.is_null() || out_width.is_null() || out_height.is_null() {
            return capture_status::ERR_NULL_ARG;
        }
        if hmonitor == 0 {
            return capture_status::ERR_BAD_VALUE;
        }
        *out_stream = 0;
        *out_width = 0;
        *out_height = 0;
        // The renderer device handles are resolved BEFORE this
        // service's state lock is taken (the lock-nesting direction
        // the services allow is renderer-lock -> capture-lock; the
        // atlas service's notify entry documents the same rule).
        let handles = match crate::renderer::shared_device() {
            Ok(handles) => handles,
            Err(code) => return code,
        };
        // IDXGIDevice -> IDirect3DDevice (the ticket's criterion: the
        // WinRT producer created from the IDXGIDevice queried from the
        // actual D3D11 device).
        let dxgi_device: IDXGIDevice = match handles.device.cast() {
            Ok(dxgi) => dxgi,
            Err(e) => {
                let code = e.code().0;
                let mut state = lock_global();
                record_last_error(&mut state, code);
                return capture_status::ERR_DEVICE;
            }
        };
        let inspectable = match CreateDirect3D11DeviceFromDXGIDevice(&dxgi_device) {
            Ok(inspectable) => inspectable,
            Err(e) => {
                let code = e.code().0;
                let mut state = lock_global();
                record_last_error(&mut state, code);
                return capture_status::ERR_DEVICE;
            }
        };
        let device: IDirect3DDevice = match inspectable.cast() {
            Ok(device) => device,
            Err(e) => {
                let code = e.code().0;
                let mut state = lock_global();
                record_last_error(&mut state, code);
                return capture_status::ERR_DEVICE;
            }
        };
        // The WinRT creation runs on the MTA housekeeping thread.
        let tx = command_channel();
        let (reply_tx, reply_rx) = channel();
        if tx
            .send(Command::CreateStream { hmonitor, device: MtaOwned::from(device), reply: reply_tx })
            .is_err()
        {
            return capture_status::ERR_DEVICE;
        }
        let (item, pool, session, width, height) = match reply_rx.recv() {
            Ok(Ok(parts)) => (
                parts.0.into_inner(),
                parts.1.into_inner(),
                parts.2.into_inner(),
                parts.3,
                parts.4,
            ),
            Ok(Err(code)) => {
                let mut state = lock_global();
                record_last_error(&mut state, code);
                return code;
            }
            Err(_) => return capture_status::ERR_DEVICE,
        };
        let shared = Arc::new(StreamShared {
            pending: Mutex::new(VecDeque::new()),
            stopped: AtomicBool::new(false),
            dropped: AtomicU32::new(0),
        });
        // Register the FrameArrived handler (free-threaded pool: the
        // event fires on a Windows thread pool thread).
        {
            let shared = shared.clone();
            let handler = windows::Foundation::TypedEventHandler::<
                Direct3D11CaptureFramePool,
                windows::core::IInspectable,
            >::new(move |sender, _args| {
                let pool = match sender.ok() {
                    Ok(pool) => pool,
                    Err(_) => return Ok(()),
                };
                if shared.stopped.load(Ordering::Acquire) {
                    return Ok(());
                }
                // Drain every available frame (the free-threaded pool
                // may coalesce arrivals).
                loop {
                    if shared.stopped.load(Ordering::Acquire) {
                        return Ok(());
                    }
                    let frame = match pool.TryGetNextFrame() {
                        Ok(frame) => frame,
                        Err(_) => return Ok(()),
                    };
                    let (texture, width, height, content_width, content_height, timestamp) =
                        match extract_frame(&frame) {
                            Ok(parts) => parts,
                            Err(_) => return Ok(()),
                        };
                    enqueue_pending(&shared, PendingFrame {
                        texture,
                        width,
                        height,
                        content_width,
                        content_height,
                        timestamp,
                    });
                }
            });
            if let Err(e) = pool.FrameArrived(&handler) {
                let code = e.code().0;
                let mut state = lock_global();
                record_last_error(&mut state, code);
                // Drop the producer objects on the MTA thread.
                let tx = command_channel();
                let (reply_tx, reply_rx) = channel();
                let _ = tx.send(Command::Drop {
                    item: Some(MtaOwned::from(item)),
                    pool: Some(MtaOwned::from(pool)),
                    session: Some(MtaOwned::from(session)),
                    reply: reply_tx,
                });
                let _ = reply_rx.recv();
                return capture_status::ERR_DEVICE;
            }
        }
        let mut state = lock_global();
        let mut take_vacant = false;
        let index = if state.slots.len() >= MAX_CAPTURE_STREAMS as usize {
            match state.slots.iter().position(|slot| !slot.occupied) {
                Some(index) => {
                    take_vacant = true;
                    index
                }
                None => return capture_status::ERR_BAD_VALUE,
            }
        } else {
            state.slots.push(CaptureSlot {
                slot_generation: 0,
                occupied: false,
                stream: None,
            });
            state.slots.len() - 1
        };
        let generation = if take_vacant {
            state.slots[index].slot_generation.wrapping_add(1) & 0xFFFFF
        } else {
            1
        };
        state.slots[index] = CaptureSlot {
            slot_generation: generation,
            occupied: true,
            stream: Some(CaptureStream {
                item: Some(item),
                pool: Some(pool),
                session: Some(session),
                shared,
                published: HashMap::new(),
                next_frame_seq: 1,
                pool_width: width,
                pool_height: height,
                size_changes: 0,
                last_error_hr: 0,
            }),
        };
        *out_stream = make_stream_handle(index, generation);
        *out_width = width;
        *out_height = height;
        capture_status::OK
    }
}

/// Extracts the frame's raw texture (an owned COM clone — the frame
/// object is dropped by the caller right after), texture size, content
/// size and timestamp.
fn extract_frame(frame: &Direct3D11CaptureFrame) -> windows::core::Result<(ID3D11Texture2D, u32, u32, i32, i32, u64)> {
    let surface = frame.Surface()?;
    let access: IDirect3DDxgiInterfaceAccess = surface.cast()?;
    let texture: ID3D11Texture2D = unsafe { access.GetInterface()? };
    let mut desc = D3D11_TEXTURE2D_DESC::default();
    unsafe { texture.GetDesc(&mut desc) };
    let content = frame.ContentSize()?;
    let timestamp = frame
        .SystemRelativeTime()
        .map(|t| t.Duration.max(0) as u64)
        .unwrap_or(0);
    Ok((texture, desc.Width, desc.Height, content.Width, content.Height, timestamp))
}

/// Enqueues one pending frame under the bounded-queue rule (drop the
/// OLDEST un-copied frames on overflow, keeping the newest — the
/// live-capture behavior; the drop counter records it).
fn enqueue_pending(shared: &Arc<StreamShared>, frame: PendingFrame) {
    let mut queue = shared
        .pending
        .lock()
        .unwrap_or_else(|poisoned| poisoned.into_inner());
    while queue.len() >= MAX_PENDING_CAPTURE_FRAMES as usize {
        match queue.pop_front() {
            Some(dropped) => {
                drop(dropped);
                shared.dropped.fetch_add(1, Ordering::Relaxed);
            }
            None => break,
        }
    }
    queue.push_back(frame);
}

fn record_last_error(state: &mut CaptureServiceState, code: i32) {
    // Attach to the most recent occupied stream (diagnostic-only;
    // creation errors have no stream yet, so the record is best
    // effort — the entry return codes stay authoritative).
    if let Some(slot) = state.slots.iter_mut().rev().find(|slot| slot.occupied) {
        if let Some(stream) = slot.stream.as_mut() {
            stream.last_error_hr = code;
        }
    }
}

fn stream_poll_body(
    stream: u64,
    out_frames: *mut GpuiGoCaptureFrameRecord,
    frame_capacity: u32,
    out_count: *mut u32,
) -> i32 {
    unsafe {
        if out_count.is_null() {
            return capture_status::ERR_NULL_ARG;
        }
        *out_count = 0;
        let (slot_index, slot_generation) = stream_handle_parts(stream);
        // The renderer device lock is the OUTERMOST lock (the
        // specified immediate-context protection for the copies; the
        // only allowed nesting direction is renderer -> capture).
        crate::renderer::with_shared_device_context(|device, context| {
            let mut state = lock_global();
            if slot_index >= state.slots.len() {
                return capture_status::ERR_BAD_HANDLE;
            }
            let slot = &mut state.slots[slot_index];
            if !slot.occupied || slot.slot_generation != slot_generation {
                return capture_status::ERR_STALE_HANDLE;
            }
            let Some(stream) = slot.stream.as_mut() else {
                return capture_status::ERR_STALE_HANDLE;
            };
            let pending_count = stream
                .shared
                .pending
                .lock()
                .unwrap_or_else(|poisoned| poisoned.into_inner())
                .len();
            if pending_count == 0 {
                return if stream.shared.stopped.load(Ordering::Acquire) {
                    capture_status::ERR_STOPPED
                } else {
                    capture_status::OK
                };
            }
            if frame_capacity == 0 || out_frames.is_null() {
                *out_count = pending_count as u32;
                return capture_status::ERR_CAPACITY;
            }
            let drainable = pending_count.min(frame_capacity as usize);
            let frames_out = std::slice::from_raw_parts_mut(out_frames, drainable);
            let mut queue = stream
                .shared
                .pending
                .lock()
                .unwrap_or_else(|poisoned| poisoned.into_inner());
            let mut written = 0usize;
            let mut status = capture_status::OK;
            while written < drainable {
                let Some(pending) = queue.pop_front() else {
                    break;
                };
                // The content-size watch: a change from the pool size
                // triggers the WGC Recreate protocol.
                if pending.content_width != stream.pool_width
                    || pending.content_height != stream.pool_height
                {
                    if let Some(pool) = stream.pool.as_ref() {
                        if let Some(device) = stream_directx_device(device) {
                            let _ = pool.Recreate(
                                &device,
                                DirectXPixelFormat::B8G8R8A8UIntNormalized,
                                CAPTURE_BUFFER_COUNT,
                                windows::Graphics::SizeInt32 {
                                    Width: pending.content_width,
                                    Height: pending.content_height,
                                },
                            );
                        }
                    }
                    stream.pool_width = pending.content_width;
                    stream.pool_height = pending.content_height;
                    stream.size_changes = stream.size_changes.saturating_add(1);
                }
                // The immutable destination: DEFAULT usage, shader
                // resource binding, written exactly once by this copy.
                let desc = D3D11_TEXTURE2D_DESC {
                    Width: pending.width,
                    Height: pending.height,
                    MipLevels: 1,
                    ArraySize: 1,
                    Format: DXGI_FORMAT_B8G8R8A8_UNORM,
                    SampleDesc: DXGI_SAMPLE_DESC { Count: 1, Quality: 0 },
                    Usage: D3D11_USAGE_DEFAULT,
                    BindFlags: D3D11_BIND_SHADER_RESOURCE.0 as u32,
                    CPUAccessFlags: 0,
                    MiscFlags: 0,
                };
                let mut dest: Option<ID3D11Texture2D> = None;
                let hr = device
                    .CreateTexture2D(&desc, None, Some(&mut dest))
                    .map_err(|e| e.code().0);
                let dest = match hr {
                    Ok(()) => dest,
                    Err(code) => {
                        stream.last_error_hr = code;
                        status = capture_status::ERR_COPY;
                        // Drop the pending frame (the copy cannot
                        // proceed for it).
                        continue;
                    }
                };
                let Some(dest) = dest else {
                    stream.last_error_hr = 0;
                    status = capture_status::ERR_COPY;
                    continue;
                };
                // The copy completes the "actual capture frame"
                // retention: from here the content is immutable.
                context.CopyResource(&dest, &pending.texture);
                let handle = make_frame_handle(
                    slot_index,
                    slot_generation,
                    stream.next_frame_seq & 0xFFFFFF,
                );
                stream.next_frame_seq = stream.next_frame_seq.wrapping_add(1);
                frames_out[written] = GpuiGoCaptureFrameRecord {
                    handle,
                    width: pending.width,
                    height: pending.height,
                    content_width: pending.content_width,
                    content_height: pending.content_height,
                    timestamp: pending.timestamp,
                    record_size: core::mem::size_of::<GpuiGoCaptureFrameRecord>() as u32,
                };
                stream.published.insert(
                    handle,
                    PublishedFrame {
                        texture: dest,
                        width: pending.width,
                        height: pending.height,
                    },
                );
                written += 1;
                // The raw pool texture's strong reference is released
                // here (the copy is complete).
            }
            *out_count = written as u32;
            status
        })
        .unwrap_or(capture_status::ERR_DEVICE)
    }
}

/// Resolves the WinRT `IDirect3DDevice` from the D3D11 device for a
/// `Recreate` call (the pool was created on it).
fn stream_directx_device(device: &ID3D11Device) -> Option<IDirect3DDevice> {
    let dxgi: IDXGIDevice = device.cast().ok()?;
    let inspectable = unsafe { CreateDirect3D11DeviceFromDXGIDevice(&dxgi) }.ok()?;
    inspectable.cast().ok()
}

fn stream_stop_body(stream: u64) -> i32 {
    let (slot_index, slot_generation) = stream_handle_parts(stream);
    let mut state = lock_global();
if slot_index >= state.slots.len() {
        return capture_status::ERR_BAD_HANDLE;
    }
    let slot = &mut state.slots[slot_index];
    if !slot.occupied || slot.slot_generation != slot_generation {
        return capture_status::ERR_STALE_HANDLE;
    }
    let Some(stream) = slot.stream.as_mut() else {
        return capture_status::ERR_STALE_HANDLE;
    };
    // Refuse new frame arrivals first (stop/close race: the
    // session stop and the handler check race safely; the queue
    // keeps anything already enqueued for the caller to drain).
    stream.shared.stopped.store(true, Ordering::Release);
    let session = match stream.session.take() {
        Some(session) => session,
        None => return capture_status::OK,
    };
    drop(state);
    let tx = command_channel();
    let (reply_tx, reply_rx) = channel();
    if tx
        .send(Command::Stop { session: MtaOwned::from(session), reply: reply_tx })
        .is_err()
    {
        return capture_status::ERR_DEVICE;
    }
    match reply_rx.recv() {
        Ok(Ok(())) => capture_status::OK,
        Ok(Err(code)) => code,
        Err(_) => capture_status::ERR_DEVICE,
    }
}

fn stream_destroy_body(stream: u64) -> i32 {
    let (slot_index, slot_generation) = stream_handle_parts(stream);
    let mut state = lock_global();
if slot_index >= state.slots.len() {
        return capture_status::ERR_BAD_HANDLE;
    }
    let slot = &mut state.slots[slot_index];
    if !slot.occupied || slot.slot_generation != slot_generation {
        return capture_status::ERR_STALE_HANDLE;
    }
    if let Some(capture_stream) = slot.stream.as_ref() {
        capture_stream.shared.stopped.store(true, Ordering::Release);
    }
    let taken = slot.stream.take();
    slot.occupied = false;
    // The published frame textures are D3D COM objects
    // (free-threaded): drop them here.
    let mut destroy_status = capture_status::OK;
    if let Some(mut stream) = taken {
        stream.published.clear();
        // The pending raw textures too.
        let mut queue = stream
            .shared
            .pending
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        queue.clear();
        drop(queue);
        // The WinRT objects drop on the MTA thread that created
        // them (apartment-affine release discipline).
        let (item, pool, session) = (stream.item.take(), stream.pool.take(), stream.session.take());
        drop(state);
        let tx = command_channel();
        let (reply_tx, reply_rx) = channel();
        if tx
            .send(Command::Drop {
                item: item.map(MtaOwned::from),
                pool: pool.map(MtaOwned::from),
                session: session.map(MtaOwned::from),
                reply: reply_tx,
            })
            .is_err()
        {
            return capture_status::ERR_DEVICE;
        }
        match reply_rx.recv() {
            Ok(Ok(())) => {}
            Ok(Err(code)) => destroy_status = code,
            Err(_) => destroy_status = capture_status::ERR_DEVICE,
        }
    }
    destroy_status
}

fn stream_stats_body(stream: u64, out_stats: *mut GpuiGoCaptureStreamStats) -> i32 {
    unsafe {
    if out_stats.is_null() {
            return capture_status::ERR_NULL_ARG;
        }
        *out_stats = GpuiGoCaptureStreamStats {
            pending: 0,
            published: 0,
            dropped: 0,
            size_changes: 0,
            stopped: 0,
            pool_width: 0,
            pool_height: 0,
            last_error_hr: 0,
            record_size: core::mem::size_of::<GpuiGoCaptureStreamStats>() as u32,
        };
        let (slot_index, slot_generation) = stream_handle_parts(stream);
        let state = lock_global();
        if slot_index >= state.slots.len() {
            return capture_status::ERR_BAD_HANDLE;
        }
        let slot = &state.slots[slot_index];
        if !slot.occupied || slot.slot_generation != slot_generation {
            return capture_status::ERR_STALE_HANDLE;
        }
        let Some(stream) = slot.stream.as_ref() else {
            return capture_status::ERR_STALE_HANDLE;
        };
        let pending = stream
            .shared
            .pending
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
            .len();
        (*out_stats).pending = pending as u32;
        (*out_stats).published = stream.published.len() as u32;
        (*out_stats).dropped = stream.shared.dropped.load(Ordering::Relaxed);
        (*out_stats).size_changes = stream.size_changes;
        (*out_stats).stopped = u32::from(stream.shared.stopped.load(Ordering::Acquire));
        (*out_stats).pool_width = stream.pool_width;
        (*out_stats).pool_height = stream.pool_height;
        (*out_stats).last_error_hr = stream.last_error_hr;
        capture_status::OK
    }
}

/// Ticket28: resolves a published frame's texture for the renderer's
/// surface draw path (an owned COM clone — the view cache keeps the
/// texture alive independently of the frame handle). Called under the
/// renderer state lock (the allowed renderer->capture nesting).
pub(crate) fn frame_texture_for_draw(
    frame_handle: u64,
) -> Result<(ID3D11Texture2D, u32, u32), i32> {
    let (slot_index, slot_generation, _seq) = frame_handle_parts(frame_handle);
    let state = lock_global();
    if slot_index >= state.slots.len() {
        return Err(capture_status::ERR_BAD_HANDLE);
    }
    let slot = &state.slots[slot_index];
    if !slot.occupied || slot.slot_generation != slot_generation {
        return Err(capture_status::ERR_STALE_HANDLE);
    }
    let Some(stream) = slot.stream.as_ref() else {
        return Err(capture_status::ERR_STALE_HANDLE);
    };
    let Some(frame) = stream.published.get(&frame_handle) else {
        return Err(capture_status::ERR_BAD_HANDLE);
    };
    Ok((frame.texture.clone(), frame.width, frame.height))
}

fn frame_release_body(frame: u64) -> i32 {
    let (slot_index, slot_generation, seq) = frame_handle_parts(frame);
    let mut state = lock_global();
if slot_index >= state.slots.len() {
        return capture_status::ERR_BAD_HANDLE;
    }
    let slot = &mut state.slots[slot_index];
    if !slot.occupied || slot.slot_generation != slot_generation {
        return capture_status::ERR_STALE_HANDLE;
    }
    let Some(stream) = slot.stream.as_mut() else {
        return capture_status::ERR_STALE_HANDLE;
    };
    // The published map is keyed by the full handle; the seq is
    // its low 24 bits (handle equality == seq + generation).
    if stream.published.remove(&frame).is_none() {
        let _ = seq;
        return capture_status::ERR_BAD_HANDLE;
    }
    capture_status::OK
}

// ---------------------------------------------------------------------------
// Panic containment and exports
// ---------------------------------------------------------------------------

macro_rules! capture_entry {
    ($body:expr) => {{
        let outcome = catch_unwind(AssertUnwindSafe(|| $body));
        match outcome {
            Ok(code) => code,
            Err(payload) => {
                drop(payload);
                capture_status::ERR_PANIC
            }
        }
    }};
}

unsafe extern "system" fn capture_supported(out_supported: *mut u32) -> i32 {
    capture_entry!(supported_body(out_supported))
}

unsafe extern "system" fn capture_sources_enumerate(
    out_records: *mut GpuiGoCaptureSourceRecord,
    record_capacity: u32,
    strings_buf: *mut u8,
    strings_capacity: u32,
    out_count: *mut u32,
    out_strings_needed: *mut u32,
) -> i32 {
    capture_entry!(sources_enumerate_body(
        out_records,
        record_capacity,
        strings_buf,
        strings_capacity,
        out_count,
        out_strings_needed
    ))
}

unsafe extern "system" fn capture_stream_create(
    hmonitor: u64,
    out_stream: *mut u64,
    out_width: *mut i32,
    out_height: *mut i32,
) -> i32 {
    capture_entry!(stream_create_body(hmonitor, out_stream, out_width, out_height))
}

unsafe extern "system" fn capture_stream_poll(
    stream: u64,
    out_frames: *mut GpuiGoCaptureFrameRecord,
    frame_capacity: u32,
    out_count: *mut u32,
) -> i32 {
    capture_entry!(stream_poll_body(stream, out_frames, frame_capacity, out_count))
}

unsafe extern "system" fn capture_stream_stop(stream: u64) -> i32 {
    capture_entry!(stream_stop_body(stream))
}

unsafe extern "system" fn capture_stream_destroy(stream: u64) -> i32 {
    capture_entry!(stream_destroy_body(stream))
}

unsafe extern "system" fn capture_stream_stats(
    stream: u64,
    out_stats: *mut GpuiGoCaptureStreamStats,
) -> i32 {
    capture_entry!(stream_stats_body(stream, out_stats))
}

unsafe extern "system" fn capture_frame_release(frame: u64) -> i32 {
    capture_entry!(frame_release_body(frame))
}

unsafe extern "system" fn capture_panic_probe() -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| {
        panic!("capture panic probe (contained by design)");
    }));
    match outcome {
        Ok(()) => capture_status::OK,
        Err(payload) => {
            drop(payload);
            capture_status::ERR_PANIC
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn handle_encoding_round_trip() {
        let handle = make_stream_handle(3, 7);
        assert_eq!(stream_handle_parts(handle), (3, 7));
        let frame = make_frame_handle(3, 7, 0x123456);
        assert_eq!(frame_handle_parts(frame), (3, 7, 0x123456));
        // Wrapping sequence stays in-bounds.
        let frame = make_frame_handle(3, 7, (1 << 30));
        assert_eq!(frame_handle_parts(frame).2, (1 << 30) & 0xFFFFFF);
    }

    #[test]
    fn record_layout_pins() {
        assert_eq!(
            core::mem::size_of::<GpuiGoCaptureSourceRecord>(),
            GpuiGoCaptureSourceRecord::default().record_size as usize
        );
        assert_eq!(
            core::mem::size_of::<GpuiGoCaptureFrameRecord>(),
            GpuiGoCaptureFrameRecord::default().record_size as usize
        );
        assert_eq!(
            core::mem::size_of::<GpuiGoCaptureStreamStats>(),
            GpuiGoCaptureStreamStats::default().record_size as usize
        );
    }

    #[test]
    fn bounded_queue_drops_oldest() {
        let shared = Arc::new(StreamShared {
            pending: Mutex::new(VecDeque::new()),
            stopped: AtomicBool::new(false),
            dropped: AtomicU32::new(0),
        });
        // A stand-in COM pointer for the queue test: a real IUnknown
        // object (its vtable's first slot is Release, exactly what Drop
        // exercises) transmuted into the texture type - the queue never
        // calls any D3D11-specific method in this test (rev 16, ticket
        // 50: the pre-existing test-only compile error -
        // ID3D11Texture2D has no Default in the windows crate).
        #[windows::core::implement(windows::Win32::System::Com::IAgileObject)]
        struct NullTexture;
        impl windows::Win32::System::Com::IAgileObject_Impl for NullTexture_Impl {}
        let null_texture: ID3D11Texture2D = unsafe {
            core::mem::transmute::<
                windows::Win32::System::Com::IAgileObject,
                ID3D11Texture2D,
            >(NullTexture.into())
        };
        for i in 0..(MAX_PENDING_CAPTURE_FRAMES + 4) {
            enqueue_pending(&shared, PendingFrame {
                texture: null_texture.clone(),
                width: i,
                height: i,
                content_width: 0,
                content_height: 0,
                timestamp: 0,
            });
        }
        let queue = shared.pending.lock().unwrap();
        assert_eq!(queue.len() as u32, MAX_PENDING_CAPTURE_FRAMES);
        assert_eq!(
            shared.dropped.load(Ordering::Relaxed),
            4,
            "the oldest frames drop on overflow"
        );
        // The newest frames survive: the front is the 5th pushed.
        assert_eq!(queue.front().unwrap().width, 4);
    }

    #[test]
    fn panic_probe_reports_containment() {
        // The export is unsafe (an extern "system" fn); the test-only
        // call needed an unsafe block (rev 16, ticket 50: the
        // pre-existing test-only compile error).
        assert_eq!(unsafe { capture_panic_probe() }, capture_status::ERR_PANIC);
    }
}
