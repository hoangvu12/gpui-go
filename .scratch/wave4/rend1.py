import sys
p = 'reference/native/src/renderer.rs'
s = open(p, encoding='utf-8').read()

def rep(old, new, count=1):
    global s
    assert s.count(old) == count, f"needle count {s.count(old)} != {count}: {old[:60]!r}"
    s = s.replace(old, new)

# 1) status code
rep('''    pub const ERR_UNSUPPORTED_SOURCE: i32 = -11;''',
'''    pub const ERR_UNSUPPORTED_SOURCE: i32 = -11;
    /// The operation was refused because the renderer is recovering
    /// from a device loss (the pin's `skip_draws` gate: draw, render,
    /// present and render-to-image skip while the replacement device's
    /// resources are not yet rebuilt; the port makes the skip
    /// observable instead of silently returning success — the Go model
    /// treats it as "frame skipped, redraw pending", matching the
    /// pin's forced-redraw recovery flow). Clear the gate with
    /// `renderer_device_mark_drawable` (the pin's `mark_drawable`).
    pub const ERR_RECOVERING: i32 = -12;''')

# 2) new records
rep('''/// Device/environment identity. The three strings (adapter description,''',
'''/// Device-loss state for `renderer_device_state_check`. Layout: `lost`
/// @0, `injected` @4, `recovering` @8, `removed_reason` @12,
/// `device_generation` @16, `record_size` @20; size 24, alignment 4.
///
/// Real and injected loss are recorded separately (the ticket's
/// requirement): `injected` marks the test injection
/// (`renderer_device_inject_loss`); `removed_reason` is the driver's
/// HRESULT for real loss, the `DXGI_ERROR_DEVICE_REMOVED` marker for
/// injected loss, or 0 when the device is alive. `device_generation`
/// counts completed rebuilds (fresh generation handles for GPU caches).
#[repr(C)]
#[derive(Clone, Copy, PartialEq, Debug)]
pub struct GpuiGoDeviceStateRecord {
    /// 1 when the device is lost (real or injected).
    pub lost: u32,
    /// 1 when the loss is the test injection, not a driver event.
    pub injected: u32,
    /// 1 while a rebuild has completed but the first forced draw has
    /// not yet marked the renderer drawable (the pin's skip_draws).
    pub recovering: u32,
    /// The removal reason HRESULT (0 = alive).
    pub removed_reason: i32,
    /// The count of completed device rebuilds.
    pub device_generation: u32,
    /// `size_of::<GpuiGoDeviceStateRecord>()` self-check.
    pub record_size: u32,
}

const _: () = assert!(std::mem::size_of::<GpuiGoDeviceStateRecord>() == 24);
const _: () = assert!(std::mem::align_of::<GpuiGoDeviceStateRecord>() == 4);

/// The rebuild result record for `renderer_device_rebuild` (the pin's
/// `handle_gpu_device_lost` outcome). Layout: `attempts` @0,
/// `rebuilt_surfaces` @4, `failed_surfaces` @8, `aborted_submissions`
/// @12, `cleared_views` @16, `device_generation` @20, `fail_reason_hr`
/// @24 (i32), `record_size` @28; size 32, alignment 4.
///
/// Pending submissions at rebuild time are ABORTED (their records are
/// removed; later poll/retire of their handles reports
/// [`renderer_status::ERR_STALE_HANDLE`]) — the safe abort of the
/// release path: the backend guarantees no further use of the old
/// device, so the in-flight markers cannot complete.
#[repr(C)]
#[derive(Clone, Copy, PartialEq, Debug)]
pub struct GpuiGoRebuildRecord {
    /// Device-creation attempts used (the pin's 5-attempt bounded
    /// retry).
    pub attempts: u32,
    /// Surfaces whose swap chains were rebuilt in place.
    pub rebuilt_surfaces: u32,
    /// Surfaces whose swap-chain recreation failed (their handles stay
    /// valid but unusable until a later rebuild succeeds).
    pub failed_surfaces: u32,
    /// Pending submissions aborted by the rebuild.
    pub aborted_submissions: u32,
    /// Capture surface views dropped from the cache (old-device SRVs).
    pub cleared_views: u32,
    /// The new device generation (GPU caches must invalidate below it).
    pub device_generation: u32,
    /// The HRESULT that failed the rebuild (0 = success).
    pub fail_reason_hr: i32,
    /// `size_of::<GpuiGoRebuildRecord>()` self-check.
    pub record_size: u32,
}

const _: () = assert!(std::mem::size_of::<GpuiGoRebuildRecord>() == 32);
const _: () = assert!(std::mem::align_of::<GpuiGoRebuildRecord>() == 4);

/// Device/environment identity. The three strings (adapter description,''')

# 3) RendererState fields
rep('''struct RendererState {
    device: Option<DeviceShared>,''',
'''struct RendererState {
    device: Option<DeviceShared>,
    /// Ticket29: the test-injected device-loss flag (recorded
    /// separately from real loss; cleared by a completed rebuild).
    injected_lost: bool,
    /// Ticket29: the pin's `skip_draws` — set by a completed rebuild,
    /// cleared by `renderer_device_mark_drawable` (the pin's
    /// `mark_drawable`, called on the first forced draw after
    /// recovery).
    recovering: bool,
    /// Ticket29: the count of completed rebuilds (fresh generation
    /// handles for GPU caches — the atlas's tile generation and the
    /// capture view cache invalidate at this boundary).''')

rep('''static GLOBAL: Mutex<RendererState> =
    Mutex::new(RendererState { device: None, draw: None, slots: Vec::new() });''',
'''static GLOBAL: Mutex<RendererState> = Mutex::new(RendererState {
    device: None,
    injected_lost: false,
    recovering: false,
    device_generation: 0,
    draw: None,
    slots: Vec::new(),
});''')

# 4) with_shared_device_context + confirmed_removed_reason
rep('''pub(super) fn shared_device() -> Result<DeviceHandles, i32> {
    let mut state = lock_global();
    ensure_device(&mut state)
}''',
'''pub(super) fn shared_device() -> Result<DeviceHandles, i32> {
    let mut state = lock_global();
    ensure_device(&mut state)
}

/// Ticket28/29: takes the renderer state lock, ensures the shared
/// device, and runs `f` with the device and immediate context WHILE
/// the lock is held — the specified immediate-context protection for
/// cross-service GPU work (the capture service's frame copies). The
/// ONLY nesting direction allowed from here is renderer-lock ->
/// capture-lock (never the reverse).
pub(super) fn with_shared_device_context<T>(
    f: impl FnOnce(&ID3D11Device, &ID3D11DeviceContext) -> T,
) -> Result<T, i32> {
    let mut state = lock_global();
    let device = ensure_device(&mut state)?;
    Ok(f(&device.device, &device.context))
}

/// Ticket29: the loss probe consulted by poll/retire and the state
/// check. Real loss comes from the driver (`GetDeviceRemovedReason`);
/// injected loss reports the `DXGI_ERROR_DEVICE_REMOVED` marker so the
/// retire-time confirmation releases failed submissions through the
/// documented loss path. None = alive.
fn confirmed_removed_reason(state: &RendererState) -> Option<i32> {
    if state.injected_lost {
        return Some(DXGI_ERROR_DEVICE_REMOVED.0 as i32);
    }
    state.device.as_ref().and_then(|shared| device_removed_reason(&shared.device))
}''')

# 5a) poll probe site — anchored with its unique predecessor
rep('''        // One bounded, nonblocking GetData call.
        let hr = poll_get_data_raw(&context, &query);
        let device_probe = state.device.as_ref().map(|device| device.device.clone());''',
'''        // One bounded, nonblocking GetData call.
        let hr = poll_get_data_raw(&context, &query);
        let device_probe = state.device.as_ref().map(|device| device.device.clone());
        let confirmed = confirmed_removed_reason(&state);''')

rep('''            record.state = match device_probe.as_ref().and_then(device_removed_reason) {
                Some(reason) => SubmissionState::Failed { reason },
                None => SubmissionState::Failed { reason: hr },
            };''',
'''            record.state = match confirmed {
                Some(reason) => SubmissionState::Failed { reason },
                None => SubmissionState::Failed { reason: hr },
            };''')

# 5b) retire probe site — anchored with its unique predecessor
rep('''    let (slot_index, slot_generation, sequence) = submission_handle_parts(submission);
    let mut state = lock_global();
    let device_probe = state.device.as_ref().map(|device| device.device.clone());''',
'''    let (slot_index, slot_generation, sequence) = submission_handle_parts(submission);
    let mut state = lock_global();
    let device_probe = state.device.as_ref().map(|device| device.device.clone());
    let confirmed = confirmed_removed_reason(&state);''')

rep('''            let Some(reason) = device_probe.as_ref().and_then(device_removed_reason) else {
                return renderer_status::ERR_QUARANTINE;
            };''',
'''            let Some(reason) = confirmed else {
                return renderer_status::ERR_QUARANTINE;
            };''')

# 6) DXGI_ERROR_DEVICE_REMOVED import
rep('''    CreateDXGIFactory2, DXGI_ADAPTER_FLAG_SOFTWARE, DXGI_CREATE_FACTORY_DEBUG,
    DXGI_CREATE_FACTORY_FLAGS, DXGI_ERROR_NOT_FOUND, DXGI_MWA_NO_ALT_ENTER, DXGI_PRESENT,''',
'''    CreateDXGIFactory2, DXGI_ADAPTER_FLAG_SOFTWARE, DXGI_CREATE_FACTORY_DEBUG,
    DXGI_CREATE_FACTORY_FLAGS, DXGI_ERROR_DEVICE_REMOVED, DXGI_ERROR_NOT_FOUND,
    DXGI_MWA_NO_ALT_ENTER, DXGI_PRESENT,''')

open(p, 'w', encoding='utf-8', newline='\n').write(s)
print("renderer.rs part 1 done")
