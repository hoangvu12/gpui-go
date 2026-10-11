import sys
p = 'reference/native/src/capture.rs'
s = open(p, encoding='utf-8').read()

def rep(old, new, count=1):
    global s
    if s.count(old) == 0 and (new in s or new.strip() == ''):
        return
    assert s.count(old) == count, f"needle count {s.count(old)} != {count}: {old[:70]!r}"
    s = s.replace(old, new)

# 1) BindFlags u32
rep('''                let desc = D3D11_TEXTURE2D_DESC {
                    Width: pending.width,
                    Height: pending.height,
                    MipLevels: 1,
                    ArraySize: 1,
                    Format: DXGI_FORMAT_B8G8R8A8_UNORM,
                    SampleDesc: DXGI_SAMPLE_DESC { Count: 1, Quality: 0 },
                    Usage: D3D11_USAGE_DEFAULT,
                    BindFlags: D3D11_BIND_SHADER_RESOURCE,
                    CPUAccessFlags: 0,
                    MiscFlags: 0,
                };''',
'''                let desc = D3D11_TEXTURE2D_DESC {
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
                };''')

# 2) stream_destroy_body: the taken stream variable
rep('''        stream.shared.stopped.store(true, Ordering::Release);
        let stream = slot.stream.take();
        slot.occupied = false;''',
'''        if let Some(capture_stream) = slot.stream.as_ref() {
            capture_stream.shared.stopped.store(true, Ordering::Release);
        }
        let taken = slot.stream.take();
        slot.occupied = false;''')
rep('''        // The published frame textures are D3D COM objects
        // (free-threaded): drop them here.
        let mut destroy_status = capture_status::OK;
        if let Some(mut stream) = stream {''',
'''        // The published frame textures are D3D COM objects
        // (free-threaded): drop them here.
        let mut destroy_status = capture_status::OK;
        if let Some(mut stream) = taken {''')

# 3) stats writes through the raw pointer
rep('''        out_stats.pending = pending as u32;
        out_stats.published = stream.published.len() as u32;
        out_stats.dropped = stream.shared.dropped.load(Ordering::Relaxed);
        out_stats.size_changes = stream.size_changes;
        out_stats.stopped = u32::from(stream.shared.stopped.load(Ordering::Acquire));
        out_stats.pool_width = stream.pool_width;
        out_stats.pool_height = stream.pool_height;
        out_stats.last_error_hr = stream.last_error_hr;''',
'''        (*out_stats).pending = pending as u32;
        (*out_stats).published = stream.published.len() as u32;
        (*out_stats).dropped = stream.shared.dropped.load(Ordering::Relaxed);
        (*out_stats).size_changes = stream.size_changes;
        (*out_stats).stopped = u32::from(stream.shared.stopped.load(Ordering::Acquire));
        (*out_stats).pool_width = stream.pool_width;
        (*out_stats).pool_height = stream.pool_height;
        (*out_stats).last_error_hr = stream.last_error_hr;''')

# 4) table size 104
rep('''const _: () = assert!(core::mem::size_of::<GpuiGoCaptureTable>() == 96);''',
'''const _: () = assert!(core::mem::size_of::<GpuiGoCaptureTable>() == 104);''')

open(p, 'w', encoding='utf-8', newline='\n').write(s)
print("capture.rs fixed 2")

# ============ draw.rs ============
p = 'reference/native/src/renderer/draw.rs'
s = open(p, encoding='utf-8').read()

def rep(old, new, count=1):
    global s
    if s.count(old) == 0 and (new in s or new.strip() == ''):
        return
    assert s.count(old) == count, f"needle count {s.count(old)} != {count}: {old[:70]!r}"
    s = s.replace(old, new)

# viewport: pass by reference
rep('''                    draw_surfaces(
                        state,
                        draw,
                        &frame,
                        viewport,
                        &snapshot.surfaces[range.clone()],
                        &snapshot.surface_opacities[range.clone()],
                    )?;''',
'''                    draw_surfaces(
                        state,
                        draw,
                        &frame,
                        &viewport,
                        &snapshot.surfaces[range.clone()],
                        &snapshot.surface_opacities[range.clone()],
                    )?;''')

# sampler: globals.sampler is already an Option
rep('''    let sampler = [Some(draw.globals.sampler.clone())];''',
'''    let sampler = [draw.globals.sampler.clone()];''')

open(p, 'w', encoding='utf-8', newline='\n').write(s)
print("draw.rs fixed 2")
