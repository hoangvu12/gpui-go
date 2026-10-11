import sys

# ============ a11y.rs fixes ============
p = 'reference/native/src/a11y.rs'
s = open(p, encoding='utf-8').read()

def rep(old, new, count=1):
    global s
    if s.count(old) == 0 and (new in s or new.strip() == ''):
        return  # already applied (idempotent skip)
    assert s.count(old) == count, f"a11y needle count {s.count(old)} != {count}: {old[:70]!r}"
    s = s.replace(old, new)

# table size 104
rep('''const _: () = assert!(core::mem::size_of::<GpuiGoA11yTable>() == 72);''',
'''const _: () = assert!(core::mem::size_of::<GpuiGoA11yTable>() == 104);''')

# publish body: field writes through the raw pointer
rep('''        let events = adapter_state.adapter.update_if_active(|| update);
        drop(state);
        out.active = 1;
        out.applied = 1;
        if let Some(events) = events {
            events.raise();
            out.events_raised = 1;
        }
        a11y_status::OK''',
'''        let events = adapter_state.adapter.update_if_active(|| update);
        drop(state);
        (*out).active = 1;
        (*out).applied = 1;
        if let Some(events) = events {
            events.raise();
            (*out).events_raised = 1;
        }
        a11y_status::OK''')

# lresult cast
rep('''                *out_lresult = lresult.0;''',
'''                *out_lresult = lresult.0 as i64;''')

# poll_actions_body: the `?` on checked_add
rep('''        let mut needed_strings = 0usize;
        for action in queue.iter().take(drainable) {
            needed_strings = needed_strings
                .checked_add(action.value.len())
                .ok_or(a11y_status::ERR_BAD_VALUE)?;
        }''',
'''        let mut needed_strings = 0usize;
        for action in queue.iter().take(drainable) {
            match needed_strings.checked_add(action.value.len()) {
                Some(total) => needed_strings = total,
                None => return a11y_status::ERR_BAD_VALUE,
            }
        }''')

open(p, 'w', encoding='utf-8', newline='\n').write(s)
print("a11y.rs fixed")

# ============ capture.rs fixes ============
p = 'reference/native/src/capture.rs'
s = open(p, encoding='utf-8').read()

def rep(old, new, count=1):
    global s
    if s.count(old) == 0 and (new in s or new.strip() == ''):
        return
    assert s.count(old) == count, f"capture needle count {s.count(old)} != {count}: {old[:70]!r}"
    s = s.replace(old, new)

# imports: MONITORINFOF_PRIMARY from WindowsAndMessaging; use core BOOL
rep('''use windows::core::Interface;
use windows::Win32::Foundation::{LPARAM, MONITORINFOF_PRIMARY};''',
'''use windows::core::{BOOL, Interface};
use windows::Win32::Foundation::LPARAM;
use windows::Win32::Graphics::Gdi::{EnumDisplayMonitors, GetMonitorInfoW, HMONITOR, MONITORINFO, MONITORINFOEXW};
use windows::Win32::UI::WindowsAndMessaging::MONITORINFOF_PRIMARY;''')

rep('''use windows::Win32::System::WinRT::{RoInitialize, RO_INIT_MULTITHREADED};''',
'''use windows::Win32::System::WinRT::{RoInitialize, RO_INIT_MULTITHREADED};''')

# BOOL return fix
rep('''unsafe extern "system" fn enum_monitors_proc(
    hmonitor: windows::Win32::Graphics::Gdi::HMONITOR,
    _hdc: windows::Win32::Graphics::Gdi::HDC,
    _rect: *mut windows::Win32::Foundation::RECT,
    lparam: LPARAM,
) -> windows::Win32::Foundation::BOOL {
    // SAFETY: EnumDisplayMonitors invokes the callback synchronously
    // on the calling thread with the LPARAM we passed.
    let ctx = unsafe { &mut *(lparam.0 as *mut EnumerateCtx) };
    ctx.monitors.push(hmonitor.0 as u64);
    windows::Win32::Foundation::BOOL(1)
}''',
'''unsafe extern "system" fn enum_monitors_proc(
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
}''')

# use the windows MONITORINFOEXW type instead of the custom struct
rep('''/// MONITORINFOEXW (the name is the friendly device string).
#[repr(C)]
struct MonitorInfoExW {
    cbSize: u32,
    rcMonitor: windows::Win32::Foundation::RECT,
    rcWork: windows::Win32::Foundation::RECT,
    dwFlags: u32,
    szDevice: [u16; 32],
}

const _: () = assert!(core::mem::size_of::<MonitorInfoExW>() == 108);

''', '''''')

rep('''            let mut info = MonitorInfoExW {
                cbSize: core::mem::size_of::<MonitorInfoExW>() as u32,
                rcMonitor: Default::default(),
                rcWork: Default::default(),
                dwFlags: 0,
                szDevice: [0; 32],
            };
            let ok = windows::Win32::Graphics::Gdi::GetMonitorInfoW(
                windows::Win32::Graphics::Gdi::HMONITOR(*hmonitor as *mut _),
                &mut info as *mut MonitorInfoExW as *mut windows::Win32::Graphics::Gdi::MONITORINFO,
            );
            let is_main = u32::from(info.dwFlags & MONITORINFOF_PRIMARY != 0);
            let label = if ok.as_bool() {
                let len = info.szDevice.iter().position(|c| *c == 0).unwrap_or(32);
                String::from_utf16_lossy(&info.szDevice[..len])
            } else {
                String::new()
            };''',
'''            let mut info = MONITORINFOEXW::default();
            info.monitorInfo.cbSize = core::mem::size_of::<MONITORINFO>() as u32;
            let ok = unsafe {
                GetMonitorInfoW(
                    HMONITOR(*hmonitor as *mut _),
                    &mut info.monitorInfo,
                )
            };
            let is_main = u32::from(info.monitorInfo.dwFlags & MONITORINFOF_PRIMARY != 0);
            let label = if ok.as_bool() {
                let len = info.szDevice.iter().position(|c| *c == 0).unwrap_or(32);
                String::from_utf16_lossy(&info.szDevice[..len])
            } else {
                String::new()
            };''')

rep('''            let mut record = GpuiGoCaptureSourceRecord {
                hmonitor: *hmonitor,
                width: info.rcMonitor.right - info.rcMonitor.left,
                height: info.rcMonitor.bottom - info.rcMonitor.top,
                is_main,
                id: index as u32 + 1,
                label_offset: label_offset as u32,
                label_len: label.len() as u32,
                record_size: core::mem::size_of::<GpuiGoCaptureSourceRecord>() as u32,
            };
            if record.record_size as usize
                != core::mem::size_of::<GpuiGoCaptureSourceRecord>()
            {
                record.record_size = core::mem::size_of::<GpuiGoCaptureSourceRecord>() as u32;
            }
            records.push(record);''',
'''            records.push(GpuiGoCaptureSourceRecord {
                hmonitor: *hmonitor,
                width: info.monitorInfo.rcMonitor.right - info.monitorInfo.rcMonitor.left,
                height: info.monitorInfo.rcMonitor.bottom - info.monitorInfo.rcMonitor.top,
                is_main,
                id: index as u32 + 1,
                label_offset: label_offset as u32,
                label_len: label.len() as u32,
                record_size: core::mem::size_of::<GpuiGoCaptureSourceRecord>() as u32,
            });''')

# EnumDisplayMonitors call
rep('''    let mut ctx = EnumerateCtx { monitors: Vec::new() };
    unsafe {
        let _ = windows::Win32::Graphics::Gdi::EnumDisplayMonitors(
            None,
            None,
            Some(enum_monitors_proc),
            LPARAM(&mut ctx as *mut _ as isize),
        );
    }
    ctx.monitors''',
'''    let mut ctx = EnumerateCtx { monitors: Vec::new() };
    unsafe {
        let _ = EnumDisplayMonitors(
            None,
            None,
            Some(enum_monitors_proc),
            LPARAM(&mut ctx as *mut _ as isize),
        );
    }
    ctx.monitors''')

# records: derive + size/align
rep('''#[repr(C)]
pub struct GpuiGoCaptureSourceRecord {''',
'''#[repr(C)]
#[derive(Clone, Copy)]
pub struct GpuiGoCaptureSourceRecord {''')
rep('''const _: () = assert!(core::mem::size_of::<GpuiGoCaptureSourceRecord>() == 36);
const _: () = assert!(core::mem::align_of::<GpuiGoCaptureSourceRecord>() == 4);''',
'''const _: () = assert!(core::mem::size_of::<GpuiGoCaptureSourceRecord>() == 40);
const _: () = assert!(core::mem::align_of::<GpuiGoCaptureSourceRecord>() == 8);''')

# table size
rep('''const _: () = assert!(core::mem::size_of::<GpuiGoCaptureTable>() == 88);''',
'''const _: () = assert!(core::mem::size_of::<GpuiGoCaptureTable>() == 96);''')

# MonitorInfoExW test pin removal
rep('''        assert_eq!(core::mem::size_of::<MonitorInfoExW>(), 108);
''', '''''')

# Command: MTA-owned wrappers for cross-thread moves
rep('''enum Command {
    /// Probe Windows Graphics Capture availability (create an item for
    /// the first monitor).
    Supported { hmonitor: u64, reply: Sender<Result<bool, i32>> },
    /// Create the frame pool + session for a monitor on the given
    /// Direct3D device.
    CreateStream {
        hmonitor: u64,
        device: IDirect3DDevice,
        reply: Sender<
            Result<
                (
                    GraphicsCaptureItem,
                    Direct3D11CaptureFramePool,
                    GraphicsCaptureSession,
                    i32,
                    i32,
                ),
                i32,
            >,
        >,
    },
    /// Stop the session (the pin's capture.stop()).
    Stop {
        session: GraphicsCaptureSession,
        reply: Sender<Result<(), i32>>,
    },
    /// Drop the producer objects (apartment-affine release on the MTA
    /// thread that created them).
    Drop {
        item: Option<GraphicsCaptureItem>,
        pool: Option<Direct3D11CaptureFramePool>,
        session: Option<GraphicsCaptureSession>,
        reply: Sender<Result<(), i32>>,
    },
}''',
'''/// A WinRT object moved across threads for MTA-affine create/drop.
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
}''')

rep('''                        Command::CreateStream { hmonitor, device, reply } => {
                            let _ = reply.send(create_stream_impl(hmonitor, &device));
                        }
                        Command::Stop { session, reply } => {
                            let result = session.StopCapture().map_err(|e| e.code().0).map(|_| ());
                            let _ = reply.send(result);
                        }
                        Command::Drop { item, pool, session, reply } => {
                            // Drop in reverse creation order; Release
                            // happens on this MTA thread.
                            drop(session);
                            drop(pool);
                            drop(item);
                            let _ = reply.send(Ok(()));
                        }''',
'''                        Command::CreateStream { hmonitor, device, reply } => {
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
                        }''')

# create_stream_impl returns MtaOwned parts
rep('''fn create_stream_impl(
    hmonitor: u64,
    device: &IDirect3DDevice,
) -> Result<(GraphicsCaptureItem, Direct3D11CaptureFramePool, GraphicsCaptureSession, i32, i32), i32>
{''',
'''fn create_stream_impl(
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
> {''')
rep('''    session.StartCapture().map_err(|e| e.code().0)?;
    Ok((item, pool, session, size.Width, size.Height))
}''',
'''    session.StartCapture().map_err(|e| e.code().0)?;
    Ok((
        MtaOwned::from(item),
        MtaOwned::from(pool),
        MtaOwned::from(session),
        size.Width,
        size.Height,
    ))
}''')

# stream_create_body: the senders/wrappers
rep('''        let tx = command_channel();
        let (reply_tx, reply_rx) = channel();
        if tx
            .send(Command::CreateStream { hmonitor, device, reply: reply_tx })
            .is_err()
        {
            return capture_status::ERR_DEVICE;
        }
        let (item, pool, session, width, height) = match reply_rx.recv() {
            Ok(Ok(parts)) => parts,
            Ok(Err(code)) => {
                let mut state = lock_global();
                record_last_error(&mut state, code);
                return code;
            }
            Err(_) => return capture_status::ERR_DEVICE,
        };''',
'''        let tx = command_channel();
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
        };''')

rep('''                let _ = tx.send(Command::Drop {
                    item: Some(item),
                    pool: Some(pool),
                    session: Some(session),
                    reply: reply_tx,
                });''',
'''                let _ = tx.send(Command::Drop {
                    item: Some(MtaOwned::from(item)),
                    pool: Some(MtaOwned::from(pool)),
                    session: Some(MtaOwned::from(session)),
                    reply: reply_tx,
                });''')

rep('''        let session = match stream.session.take() {
            Some(session) => session,
            None => return capture_status::OK,
        };
        drop(state);
        let tx = command_channel();
        let (reply_tx, reply_rx) = channel();
        if tx.send(Command::Stop { session, reply: reply_tx }).is_err() {
            return capture_status::ERR_DEVICE;
        }''',
'''        let session = match stream.session.take() {
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
        }''')

rep('''            let (item, pool, session) = (stream.item.take(), stream.pool.take(), stream.session.take());
            drop(state);
            let tx = command_channel();
            let (reply_tx, reply_rx) = channel();
            if tx
                .send(Command::Drop { item, pool, session, reply: reply_tx })
                .is_err()
            {''',
'''            let (item, pool, session) = (stream.item.take(), stream.pool.take(), stream.session.take());
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
            {''')

# frame arrival closure: sender.ok() is Result
rep('''                let pool = match sender.ok() {
                    Some(pool) => pool,
                    None => return Ok(()),
                };''',
'''                let pool = match sender.ok() {
                    Ok(pool) => pool,
                    Err(_) => return Ok(()),
                };''')

open(p, 'w', encoding='utf-8', newline='\n').write(s)
print("capture.rs fixed")

# ============ renderer.rs fixes ============
p = 'reference/native/src/renderer.rs'
s = open(p, encoding='utf-8').read()

def rep(old, new, count=1):
    global s
    if s.count(old) == 0 and (new in s or new.strip() == ''):
        return
    assert s.count(old) == count, f"renderer needle count {s.count(old)} != {count}: {old[:70]!r}"
    s = s.replace(old, new)

# the missing device_generation field
rep('''    /// capture view cache invalidate at this boundary).
    /// Ticket16: the device-global draw state (pipelines and global''',
'''    /// capture view cache invalidate at this boundary).
    device_generation: u32,
    /// Ticket16: the device-global draw state (pipelines and global''')

# the table size pin
rep('''const _: () = assert!(std::mem::size_of::<GpuiGoRendererTable>() == 128);''',
'''const _: () = assert!(std::mem::size_of::<GpuiGoRendererTable>() == 176);''')

# unused device_probe variables (superseded by `confirmed`)
rep('''        let device_probe = state.device.as_ref().map(|device| device.device.clone());
        let confirmed = confirmed_removed_reason(&state);''',
'''        let confirmed = confirmed_removed_reason(&state);''')
rep('''    let device_probe = state.device.as_ref().map(|device| device.device.clone());
    let confirmed = confirmed_removed_reason(&state);''',
'''    let confirmed = confirmed_removed_reason(&state);''')

open(p, 'w', encoding='utf-8', newline='\n').write(s)
print("renderer.rs fixed")

# ============ draw.rs fixes ============
p = 'reference/native/src/renderer/draw.rs'
s = open(p, encoding='utf-8').read()

def rep(old, new, count=1):
    global s
    if s.count(old) == 0 and (new in s or new.strip() == ''):
        return
    assert s.count(old) == count, f"draw needle count {s.count(old)} != {count}: {old[:70]!r}"
    s = s.replace(old, new)

rep('''    let active_keys: std::collections::HashSet<usize> = surfaces
        .iter()
        .filter(|surface| surface.source_tag == surface_source_kind::CAPTURE_FRAME)
        .map(|surface| surface.source_reserved[0] as u64)
        .collect();''',
'''    let active_keys: std::collections::HashSet<u64> = surfaces
        .iter()
        .filter(|surface| surface.source_tag == surface_source_kind::CAPTURE_FRAME)
        .map(|surface| surface.source_reserved[0] as u64)
        .collect();''')

rep('''    let mut resolved: std::collections::HashMap<u64, usize> =
        std::collections::HashMap::new();
    for handle in &active_keys {
        if let Ok((texture, _, _)) = crate::capture::frame_texture_for_draw(*handle) {
            resolved.insert(*handle, Interface::as_raw(&texture) as usize);
        }
    }
    let keep: std::collections::HashSet<usize> =
        resolved.values().copied().collect();
    draw.surface_views.retain(|key, _| keep.contains(key));''',
'''    let mut resolved: std::collections::HashMap<u64, usize> =
        std::collections::HashMap::new();
    for handle in &active_keys {
        if let Ok((texture, _, _)) = crate::capture::frame_texture_for_draw(*handle) {
            resolved.insert(*handle, Interface::as_raw(&texture) as usize);
        }
    }
    let keep: std::collections::HashSet<usize> =
        resolved.values().copied().collect();
    draw.surface_views.retain(|key, _| keep.contains(key));'''.replace('        .map(|surface| surface.source_reserved[0] as u64)\n', '        .map(|surface| surface.source_reserved[0] as u64)\n'))

open(p, 'w', encoding='utf-8', newline='\n').write(s)
print("draw.rs fixed")
