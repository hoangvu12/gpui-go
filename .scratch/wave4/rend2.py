import sys
p = 'reference/native/src/renderer.rs'
s = open(p, encoding='utf-8').read()

def rep(old, new, count=1):
    global s
    assert s.count(old) == count, f"needle count {s.count(old)} != {count}: {old[:70]!r}"
    s = s.replace(old, new)

# A) Service version bump + doc
rep('''/// Renderer service ABI version (bumped only on record/table layout
/// changes of the renderer service). Version 2 (ticket16) added the
/// scene drawing entries (`surface_draw_scene` / `surface_render_scene`
/// / `surface_read_pixels`) and the draw-constant scalars
/// (`max_instance_buffer_bytes`, `path_multisample_count`).
pub const GPUI_GO_RENDERER_SERVICE_VERSION: u32 = 2;''',
'''/// Renderer service ABI version (bumped only on record/table layout
/// changes of the renderer service). Version 2 (ticket16) added the
/// scene drawing entries (`surface_draw_scene` / `surface_render_scene`
/// / `surface_read_pixels`) and the draw-constant scalars
/// (`max_instance_buffer_bytes`, `path_multisample_count`). Version 3
/// (ticket28/29/32, the wave-4 rebuild) added the device-loss entries
/// (`device_state_check` / `device_inject_loss` / `device_rebuild` /
/// `device_mark_drawable`), the test-support render-to-image entry
/// (`surface_render_to_image`) and its record size.
pub const GPUI_GO_RENDERER_SERVICE_VERSION: u32 = 3;''')

# B) Table fields: after surface_read_pixels, before service_version
rep('''    /// Renderer ABI version (see [`GPUI_GO_RENDERER_SERVICE_VERSION`]).
    pub service_version: u32,''',
'''    /// `renderer_surface_render_to_image` (ticket32): the pinned
    /// test-support path — render the scene without presenting, then
    /// stage-copy the render target into owned RGBA bytes (the pin's
    /// `render_to_image`, including its device-recovery refusal).
    pub surface_render_to_image: Option<
        unsafe extern "system" fn(
            surface: u64,
            scene: u64,
            appearance: u32,
            atlas: u64,
            out_bytes: *mut u8,
            capacity: u32,
            out_needed: *mut u32,
        ) -> i32,
    >,
    /// `renderer_device_state_check` (ticket29): the live device-loss
    /// state (real + injected + recovering, the removal reason, the
    /// device generation).
    pub device_state_check:
        Option<unsafe extern "system" fn(out: *mut GpuiGoDeviceStateRecord) -> i32>,
    /// `renderer_device_inject_loss` (ticket29, test-only): arm the
    /// injected-loss flag (recorded separately from real loss).
    pub device_inject_loss: Option<unsafe extern "system" fn() -> i32>,
    /// `renderer_device_rebuild` (ticket29): the pin's bounded-retry
    /// device recovery — recreate the device, rebuild every surface's
    /// swap chain in place, abort in-flight submissions, drop the draw
    /// globals and the capture view cache, arm the recovery gate.
    pub device_rebuild: Option<unsafe extern "system" fn(out: *mut GpuiGoRebuildRecord) -> i32>,
    /// `renderer_device_mark_drawable` (ticket29): clear the recovery
    /// gate (the pin's `mark_drawable`, called on the first forced
    /// draw after recovery).
    pub device_mark_drawable: Option<unsafe extern "system" fn() -> i32>,
    /// Renderer ABI version (see [`GPUI_GO_RENDERER_SERVICE_VERSION`]).
    pub service_version: u32,'''
)

# also the record-size scalars: after size_of_device_info
rep('''    /// `size_of::<GpuiGoDeviceInfoRecord>()` self-check.
    pub size_of_device_info: u32,''',
'''    /// `size_of::<GpuiGoDeviceInfoRecord>()` self-check.
    pub size_of_device_info: u32,
    /// `size_of::<GpuiGoDeviceStateRecord>()` self-check (ticket29).
    pub size_of_device_state: u32,
    /// `size_of::<GpuiGoRebuildRecord>()` self-check (ticket29).
    pub size_of_rebuild_record: u32,''')

# C) Table values
rep('''    surface_draw_scene: Some(renderer_surface_draw_scene),
    surface_render_scene: Some(renderer_surface_render_scene),
    surface_read_pixels: Some(renderer_surface_read_pixels),
    service_version: GPUI_GO_RENDERER_SERVICE_VERSION,''',
'''    surface_draw_scene: Some(renderer_surface_draw_scene),
    surface_render_scene: Some(renderer_surface_render_scene),
    surface_read_pixels: Some(renderer_surface_read_pixels),
    surface_render_to_image: Some(renderer_surface_render_to_image),
    device_state_check: Some(renderer_device_state_check),
    device_inject_loss: Some(renderer_device_inject_loss),
    device_rebuild: Some(renderer_device_rebuild),
    device_mark_drawable: Some(renderer_device_mark_drawable),
    service_version: GPUI_GO_RENDERER_SERVICE_VERSION,''')

rep('''    size_of_device_info: std::mem::size_of::<GpuiGoDeviceInfoRecord>() as u32,
    max_pending_submissions: MAX_PENDING_SUBMISSIONS,''',
'''    size_of_device_info: std::mem::size_of::<GpuiGoDeviceInfoRecord>() as u32,
    size_of_device_state: std::mem::size_of::<GpuiGoDeviceStateRecord>() as u32,
    size_of_rebuild_record: std::mem::size_of::<GpuiGoRebuildRecord>() as u32,
    max_pending_submissions: MAX_PENDING_SUBMISSIONS,''')

# D) surface_present recovery gate
rep('''    let (slot_index, slot_generation) = surface_handle_parts(surface);
    let mut state = lock_global();
    let device = match ensure_device(&mut state) {
        Ok(device) => device,
        Err(code) => return code,
    };
    let surface_state = match resolve_slot_mut(&mut state, slot_index, slot_generation) {
        Ok(surface) => surface,
        Err(code) => return code,
    };''',
'''    let (slot_index, slot_generation) = surface_handle_parts(surface);
    let mut state = lock_global();
    let device = match ensure_device(&mut state) {
        Ok(device) => device,
        Err(code) => return code,
    };
    // Ticket29: the recovery gate (the pin's skip_draws in `draw`).
    if state.recovering {
        return renderer_status::ERR_RECOVERING;
    }
    let surface_state = match resolve_slot_mut(&mut state, slot_index, slot_generation) {
        Ok(surface) => surface,
        Err(code) => return code,
    };''')

# E) the new bodies + exports, inserted before the test module
rep('''#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn table_self_check_fields_match_memory_truth() {''',
'''// ---------------------------------------------------------------------------
// Ticket29: device loss, recovery and rebuild
// ---------------------------------------------------------------------------

/// `renderer_device_state_check`.
fn device_state_check_body(out: *mut GpuiGoDeviceStateRecord) -> i32 {
    if out.is_null() {
        return renderer_status::ERR_NULL_ARG;
    }
    let state = lock_global();
    let confirmed = confirmed_removed_reason(&state);
    unsafe {
        *out = GpuiGoDeviceStateRecord {
            lost: u32::from(confirmed.is_some()),
            injected: u32::from(state.injected_lost),
            recovering: u32::from(state.recovering),
            removed_reason: confirmed.unwrap_or(0),
            device_generation: state.device_generation,
            record_size: std::mem::size_of::<GpuiGoDeviceStateRecord>() as u32,
        };
    }
    renderer_status::OK
}

/// `renderer_device_inject_loss` (test-only).
fn device_inject_loss_body() -> i32 {
    let mut state = lock_global();
    state.injected_lost = true;
    renderer_status::OK
}

/// `renderer_device_rebuild`: the pin's `handle_gpu_device_lost` +
/// `handle_device_lost_impl` (platform.rs 1427-1475, window.rs
/// 452-530) adapted to this service's single-device/multi-surface
/// shape:
///
/// * the pin's 350ms settle before the recovery loop;
/// * `try_to_recover_from_device_lost`'s 5-attempt bounded retry
///   (100ms + i*10ms backoff between attempts);
/// * the old state is torn down (unbound targets, ClearState, Flush),
///   the device is recreated, every surface's swap chain and
///   composition tree are rebuilt IN PLACE (the pin rebuilds each
///   window's renderer object's resources — surface identity and
///   handles persist), the draw globals and capture surface views are
///   dropped for lazy rebuild, in-flight submissions are aborted (the
///   old device guarantees no further use);
/// * `skip_draws` is armed (this service's `recovering`) for the first
///   forced draw to clear via `renderer_device_mark_drawable`.
fn device_rebuild_body(out: *mut GpuiGoRebuildRecord) -> i32 {
    if out.is_null() {
        return renderer_status::ERR_NULL_ARG;
    }
    unsafe {
        *out = GpuiGoRebuildRecord {
            attempts: 0,
            rebuilt_surfaces: 0,
            failed_surfaces: 0,
            aborted_submissions: 0,
            cleared_views: 0,
            device_generation: 0,
            fail_reason_hr: 0,
            record_size: std::mem::size_of::<GpuiGoRebuildRecord>() as u32,
        };
    }
    // The pin's settle: give the system time to recover from the lost
    // state ("if we don't wait, the final drawing result will be
    // blank").
    std::thread::sleep(std::time::Duration::from_millis(350));
    let mut state = lock_global();
    // Tear down the old device state (the pin's handle_device_lost_impl
    // head: unbind, ClearState, Flush).
    if let Some(shared) = state.device.as_ref() {
        unsafe {
            shared.context.OMSetRenderTargets(None, None);
            shared.context.ClearState();
            shared.context.Flush();
        }
    }
    // Drop the draw globals and the capture surface views (both are
    // lazily rebuilt on the replacement device).
    state.draw = None;
    let cleared_views = draw::clear_surface_views(&mut state);
    // Abort every in-flight submission: their queries reference the
    // lost device; the rebuild is the backend's safe-abort guarantee.
    let mut aborted_submissions = 0u32;
    for slot in state.slots.iter_mut() {
        if let Some(surface) = slot.surface.as_mut() {
            aborted_submissions += surface.submissions.drain().count() as u32;
        }
    }
    // Recreate the device with the pin's bounded retry
    // (try_to_recover_from_device_lost: 5 attempts, the first without
    // delay, 100ms + i*10ms between the rest).
    state.device = None;
    let mut attempts = 0u32;
    let mut fail_reason = 0i32;
    loop {
        attempts += 1;
        match ensure_device(&mut state) {
            Ok(_) => break,
            Err(code) => {
                fail_reason = code;
                if attempts >= 5 {
                    // Terminal: the rebuild failed; the old device is
                    // gone, surfaces stay unusable until a later
                    // rebuild succeeds.
                    unsafe {
                        (*out).attempts = attempts;
                        (*out).fail_reason_hr = fail_reason;
                        (*out).aborted_submissions = aborted_submissions;
                        (*out).cleared_views = cleared_views;
                    }
                    return renderer_status::ERR_DEVICE;
                }
                std::thread::sleep(std::time::Duration::from_millis(
                    100 + (attempts as u64) * 10,
                ));
            }
        }
    }
    // Rebuild every occupied surface's swap chain, views and
    // composition tree in place (the pin rebuilds each renderer's
    // resources; handles persist).
    let mut rebuilt_surfaces = 0u32;
    let mut failed_surfaces = 0u32;
    for slot in state.slots.iter_mut() {
        let Some(surface) = slot.surface.as_mut() else {
            continue;
        };
        let hwnd_handle = HWND(surface.hwnd as *mut core::ffi::c_void);
        let mode = surface.mode;
        let width = surface.width;
        let height = surface.height;
        let device = match state.device.as_ref() {
            Some(shared) => shared,
            None => {
                failed_surfaces += 1;
                continue;
            }
        };
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
            Err(code) => {
                failed_surfaces += 1;
                if fail_reason == 0 {
                    fail_reason = code;
                }
                continue;
            }
        };
        let (render_target, render_target_view) =
            match create_render_target_and_its_view(&swap_chain, &device.device) {
                Ok(pair) => pair,
                Err(code) => {
                    failed_surfaces += 1;
                    if fail_reason == 0 {
                        fail_reason = code;
                    }
                    continue;
                }
            };
        let composition = match mode {
            surface_mode::DCOMP_PREMULTIPLIED => {
                match create_composition_tree(&device.device, hwnd_handle, &swap_chain) {
                    Ok(tree) => Some(tree),
                    Err(code) => {
                        failed_surfaces += 1;
                        if fail_reason == 0 {
                            fail_reason = code;
                        }
                        continue;
                    }
                }
            }
            _ => None,
        };
        surface.swap_chain = swap_chain;
        surface.render_target = Some(render_target);
        surface.render_target_view = Some(render_target_view);
        surface.viewport = set_viewport(&device.context, width as f32, height as f32);
        surface.composition = composition;
        surface.draw_targets = draw::DrawTargets::default();
        surface.resize_generation = surface.resize_generation.saturating_add(1);
        rebuilt_surfaces += 1;
    }
    // The recovery gate: the pin's skip_draws = true (armed until the
    // first forced draw's mark_drawable).
    state.recovering = true;
    state.injected_lost = false;
    state.device_generation = state.device_generation.saturating_add(1);
    unsafe {
        (*out).attempts = attempts;
        (*out).rebuilt_surfaces = rebuilt_surfaces;
        (*out).failed_surfaces = failed_surfaces;
        (*out).aborted_submissions = aborted_submissions;
        (*out).cleared_views = cleared_views;
        (*out).device_generation = state.device_generation;
        (*out).fail_reason_hr = fail_reason;
    }
    renderer_status::OK
}

/// `renderer_device_mark_drawable` (the pin's `mark_drawable`).
fn device_mark_drawable_body() -> i32 {
    let mut state = lock_global();
    state.recovering = false;
    renderer_status::OK
}

unsafe extern "system" fn renderer_device_state_check(
    out: *mut GpuiGoDeviceStateRecord,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| device_state_check_body(out)));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            renderer_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn renderer_device_inject_loss() -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| device_inject_loss_body()));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            renderer_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn renderer_device_rebuild(out: *mut GpuiGoRebuildRecord) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| device_rebuild_body(out)));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            renderer_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn renderer_device_mark_drawable() -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| device_mark_drawable_body()));
    match outcome {
        Ok(code) => code,
        Err(payload) => {
            drop(payload);
            renderer_status::ERR_PANIC
        }
    }
}

unsafe extern "system" fn renderer_surface_render_to_image(
    surface: u64,
    scene: u64,
    appearance: u32,
    atlas: u64,
    out_bytes: *mut u8,
    capacity: u32,
    out_needed: *mut u32,
) -> i32 {
    let outcome = catch_unwind(AssertUnwindSafe(|| {
        draw::surface_render_to_image_body(surface, scene, appearance, atlas, out_bytes, capacity, out_needed)
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
    fn table_self_check_fields_match_memory_truth() {''')

# F) tests: new status code + new table slots + new record sizes
rep('''            renderer_status::ERR_CAPACITY,
            renderer_status::ERR_PANIC,
        ];''',
'''            renderer_status::ERR_CAPACITY,
            renderer_status::ERR_RECOVERING,
            renderer_status::ERR_PANIC,
        ];''')

rep('''        assert_eq!(RENDERER_TABLE.max_pending_submissions, MAX_PENDING_SUBMISSIONS);
    }''',
'''        assert_eq!(RENDERER_TABLE.max_pending_submissions, MAX_PENDING_SUBMISSIONS);
        assert_eq!(
            RENDERER_TABLE.size_of_device_state as usize,
            std::mem::size_of::<GpuiGoDeviceStateRecord>()
        );
        assert_eq!(
            RENDERER_TABLE.size_of_rebuild_record as usize,
            std::mem::size_of::<GpuiGoRebuildRecord>()
        );
    }''')

rep('''            RENDERER_TABLE.device_info.map(|f| f as usize).unwrap_or(0),
        ];
        assert!(slots.iter().all(|slot| *slot != 0));
    }''',
'''            RENDERER_TABLE.device_info.map(|f| f as usize).unwrap_or(0),
            RENDERER_TABLE.surface_render_to_image.map(|f| f as usize).unwrap_or(0),
            RENDERER_TABLE.device_state_check.map(|f| f as usize).unwrap_or(0),
            RENDERER_TABLE.device_inject_loss.map(|f| f as usize).unwrap_or(0),
            RENDERER_TABLE.device_rebuild.map(|f| f as usize).unwrap_or(0),
            RENDERER_TABLE.device_mark_drawable.map(|f| f as usize).unwrap_or(0),
        ];
        assert!(slots.iter().all(|slot| *slot != 0));
    }''')

open(p, 'w', encoding='utf-8', newline='\n').write(s)
print("renderer.rs part 2 done")
