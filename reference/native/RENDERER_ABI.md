# gpui-go native renderer service ABI (v2)

**Renderer service version 6 (rev 16, browser-core ticket 53, native
revision 151 - ticket 54 bumps the revision):** the retained frame,
the damage redraw and the dirty-rect present (the design's §3.11). The
table appends TWO entries after `surface_debug_stats` (21 function
pointers, table size 224) plus two scalars (`size_of_damage_rect`,
`max_damage_regions` = zgui's 16-region cap):

```
i32 renderer_surface_draw_scene_damage(u64 surface, u64 scene,
    u32 appearance, u64 atlas, const GpuiGoRectRecord* rects,
    u32 count, u32 flags /* bit 0 force full */, u64* out_submission)

i32 renderer_surface_read_frame_pixels(u64 surface, u8* out_bytes,
    u32 capacity, u32* out_needed)
```

`GpuiGoRectRecord` = `{x, y, w, h}` i32, 16 bytes. The damage entry:
the offscreen scene texture becomes the RETAINED FRAME TARGET (it
persists per surface across frames and always holds the last
presented frame - a new/resized/recovered surface, a clear-present or
a legacy direct-to-swapchain draw invalidates it and forces a full
redraw, tracked per surface). The rects are clamped to the viewport,
EXPANDED across every blur/drop-shadow group, mask, transform and
backdrop whose region they intersect (viz's
`ExpandDamageForPixelMovingFilters` rule; the blur dilation is the
pin's composite dilation 3*radius; drop-shadow ops use their shadow
quad; masks/transforms their group footprint), MERGED (nearby rects,
the 8-px merge gap) and CAPPED at 16 regions (zgui's `MAX_REGIONS` -
over the cap, or an empty clamp result, redraws fully; there is NO
percentage-of-window threshold). The render: a SCISSORED redraw of
the damage union into the retained target (the scissor-enabled
rasterizer state; the clear is a scissored constant pass - a 1x1
texture through the downsample pipeline, both clear colors exact;
groups whose region intersects the damage box render their buckets
UNSCISzerED so the blur's kernel reach and the composites' bilinear
support read the full content). The present: each damage rect is
copied from the retained target into the back buffer
(`CopySubresourceRegion` - DXGI requires the app to have written
every pixel of each dirty rect; the retained target makes the copy
independent of buffer age, Chromium's own Windows pattern,
`software_output_device_win_swapchain.cc:241-273`), then `Present1`
with those dirty rects. A Present1 failure falls back to a full
present. In DirectComposition mode (mode 0) the partial present is
used only after the ONE-SHOT readback test passes (the next back
buffer must hold the composed frame); a failure falls back to full
presents permanently (the design's Q5 answer; on the R16 test
machine the DComp verification fails and the fallback fires, while
the hwnd mode accepts Present1 - Chromium's own software path).

`renderer_surface_read_frame_pixels` is the TEST SEAM: the staging
readback of the RETAINED FRAME TARGET (the last presented frame,
independent of the swap chain's rotation) - the damaged-vs-full
redraw equivalence gates read it; ERR_DEVICE before the first damage
draw creates the target.

**Ticket 53 also extends the debug-stats record (append-only after
`record_size`, 16 -> 36 bytes):** `state_bind_count` (the §3.12
tracker's cumulative issued binds - DEVICE-GLOBAL like the tracker,
the shared immediate context keeps one pipeline state across
surfaces and frames), `tess_cache_hits` (the scene service's
global counter), `damage_fraction` (the LAST frame's merged damage
area over the window area, per-mille, 1000 = full) and
`full_present_count`/`partial_present_count` (the damage entry's
cumulative presents, per-surface).

**Ticket 53 finishes 51's handed-off filter-bucket gap:** FILTER
groups move to BOUNDS-SIZED buckets too (the region's far corner,
quantized, clamped to the window - a region reaching the window edge
keeps R15's window-sized bucket and its window blur pair), because
the blur chains are now REGION-LIMITED: the downsample/gaussian
passes shade only the composite region plus the kernel reach (the
scissor - NOT a viewport offset: the fullscreen blur passes sample
viewport-relative texture coordinates, so an offset viewport samples
wrong texels), reading the full source, so the in-region values are
bit-identical to the full-window chain (the gaussian is local, every
in-region read stays inside the shaded region plus the reach margin
the regions carry). The bucket carries the margin the chain reads:
the content extent, the record bounds dilated by 9*radius (the
composite's 3σ plus the chain's read reach) and each drop-shadow
op's shadow quad dilated by its own reach. The R15 blur fixtures stay
bit-exact (the parity filters/shadow/gradient packages re-verified).
The §3.12 state-bind tracker (draw_with_variant/dx_blur_pass bind
only what changed; every raw bind of a tracked slot elsewhere
invalidates) rides the same version: zero pixel impact, observable
through `state_bind_count`.

**Renderer service version 5 (rev 16, browser-core ticket 50, native
revision 151 - ticket 54 bumps the revision):** the R16 group machinery.
The design's "renderer service v5" is this version. The table appends
`surface_debug_stats` after `surface_frame_latency_handle` (18 function
pointers, table size 200) with its record `GpuiGoDebugStatsRecord` (16
bytes: `pool_target_count`, `pool_bytes`, `pool_overflow_count`,
`record_size`) - the per-surface group-target pool observability the
R16 design requires ("over the budget a group renders inline,
observably"). The draw path grows the R16 composite: groups composite through
POOLED targets with no depth cap (the pool is retained across frames,
pruned of entries no group used, capped at 64 targets / 256 MB per
surface with the inline demotion above), effect groups composite
through the explicit no-blur `composite_group` path (opacity +
rounded clip), and the backdrop crop carries its own crop-sized
half-resolution blur pair (ticket 77's partial-region backdrop zoom
fix - a full-window backdrop keeps identical sizes and pixels).

**Ticket 51 finishes the bounds-sized buckets (the 50 deviation
resolved):** the pool's buckets are now BOUNDS-SIZED, quantized to the
256-px granularity and clamped to the window. The root cause of 50's
sprite gap: the sketched offset-viewport scheme (children at window
coordinates, the viewport shifted by the bucket origin) breaks every
pipeline whose fragment compares SV_Position against the records'
coordinate space - the polychrome sprite's rounded-rect SDF and the
content-mask fades, the vendored quads'/shadows' border/radius
geometry, the underline wave all read window-space records against
target-frame pixels and the per-pixel geometry drops the whole
primitive (a rounded quad draws nothing; 50's plain-quad control
passed only through the borderless early-out). The buckets therefore
render at their FRAME'S ORIGIN with the GLOBALS' viewport size set to
the bucket: the children keep their true coordinates, every
fragment's SV_Position stays in the records' frame (the pin's own
invariant, which its pre_draw establishes for the window), and the
composite draws the group's CONTENT REGION (the bucket's un-quantized
extent) sampling the bucket at (region-relative position) / bucket
size. A transform group's bucket is its LOCAL extent (the subtree's
records are local-frame, mapped by the composite's 2x3); a
window-frame group's bucket spans the frame's [0, extent max] (the
region's distance from the origin inflates it - the price of the
invariant); FILTER groups keep window-sized buckets (R15's exact blur
textures and scratch - a bounds-sized filter bucket would wrap the
gaussian at the region edge and shift the R15 blur fixtures' edge
pixels; ticket 53's region-limited blur owns that change). The
composite's 2x3 rides the vendored blur-composite extension cbuffer
(b5, appended fields; the rev-15.1 prefix is byte-identical).

**Renderer service version 4 (gpui-perf ticket 05, native revision 13):**
every swap chain (both modes, and the device-loss rebuild) is created
with `DXGI_SWAP_CHAIN_FLAG_FRAME_LATENCY_WAITABLE_OBJECT` (and
`ResizeBuffers` passes the same flag), `IDXGISwapChain2::
SetMaximumFrameLatency(1)` is applied, and the surface keeps the
waitable handle (closed with the surface or on rebuild). The table
gained `surface_frame_latency_handle(surface: u64, out_handle: *mut u64)
-> i32` after `device_mark_drawable`: it writes a caller-owned
`DuplicateHandle` copy of the waitable (0 when the swap chain exposes no
`IDXGISwapChain2`), so the caller waits outside the renderer lock and
closes it. Table size 184. Deviation from the pin, which creates its
swap chains with `Flags: 0` and default latency. Present stays
`Present(0, 0)` for the legacy draw path; `Present1` dirty rects are
not used WITHOUT a damage source (see the ticket 05 answer: the
flip-model back buffer is three frames old and the legacy draw path
redraws the whole target, so a dirty-rect hint has nothing to be
correct against) - ticket 53's damage entry IS the damage source: it
keeps the retained frame target, copies the damage rects into the
back buffer and presents them through `Present1` (see the version 6
paragraph above).

Design contract for ticket07, extended by ticket16 (the scene draw
pipeline). This document is the authoritative
interface between the Rust native renderer service
(`reference/native/src/renderer.rs`) and the Go adapters
(`internal/native/renderer.go`, `gpui/renderer.go`). It follows the
conventions of [LAYOUT_ABI.md](LAYOUT_ABI.md); deviations and additions
are marked. Source of truth for semantics: the
[renderer contract](../../docs/renderer-contract.md) and the pinned CE
sources (`crates/gpui_windows/src/directx_devices.rs`,
`directx_renderer.rs`, `platform.rs::check_device_lost`,
`directx_renderer.rs` gpu_specs/nvidia/amd/dxgi at `254b5dbd…`).

The service occupies **reserved slot 2** of the `GpuiGoAbiTable`
(bit-2 pointer to the static `GpuiGoRendererTable`). Capability bit 2 =
`renderer-d3d11`. The artifact's native revision is 3 and the
capability mask is 7 (bootstrap + layout + renderer); the new artifact is
a new artifact identity and was re-measured and re-embedded.

## General rules (identical to the layout service)

- C ABI, `extern "system"`, `#[repr(C)]`, fixed-width types only. No Rust
  structs by value across the boundary, no Rust Vec/String/Option/enums,
  no booleans (u32 0/1), **no Go pointers retained by native code** (the
  renderer service has no Go callbacks at all: completion is polled, not
  trampolined).
- Every entry returns a `Status` (i32): 0 ok; negative = caller/argument
  error with one documented exception (`-9`, below); 100+ = internal
  failure. Never a silent wrong result.
- Panics are contained with `catch_unwind` in every export; a contained
  panic returns `ERR_PANIC` (101) and does not poison the state mutex.
- Handles are validated opaque u64s:
  - **SurfaceHandle** = `(slot index << 20) | slot generation` — like an
    engine handle; stale after destroy (slot generation persists across
    vacancy).
  - **SubmissionHandle** = `(slot index << 52) | (slot generation << 32) |
    submission sequence` (12-bit slot, 20-bit generation, 32-bit
    per-surface sequence, never reused).
  - Malformed or never-issued handles return `ERR_BAD_HANDLE`; past
    generation or already-retired handles return `ERR_STALE_HANDLE`.
- Immediate-context serialization: **one service-global mutex** guards the
  shared device, every surface and every submission record. Every export
  takes it; no lock is held across a callback (there are none). D3D11
  multithread protection is additionally set on the immediate context
  (renderer contract) and reported through the device-info record.
- Native staging ownership: the only caller-provided pointer arguments
  are value records and device-info byte buffers, borrowed for the
  duration of one call. Forced GC can never observe a dangling native
  reference.

## Pinned semantics ported

- **Device** (`directx_devices.rs`): `IDXGIFactory6` from
  `CreateDXGIFactory2` (debug flags only when the debug layer is
  available — release builds never probe); adapter enumeration until
  `DXGI_ERROR_NOT_FOUND`; per adapter `D3D11CreateDevice` with
  `D3D_DRIVER_TYPE_UNKNOWN`, `D3D11_CREATE_DEVICE_BGRA_SUPPORT`
  (+ `D3D11_CREATE_DEVICE_DEBUG` under the debug layer), feature levels
  `[11_1, 11_0]`, `D3D11_SDK_VERSION`; first success wins and is
  recorded. No compatible adapter is `ERR_DEVICE` — no WARP fallback.
- **Swap chains** (`directx_renderer.rs`): format
  `DXGI_FORMAT_B8G8R8A8_UNORM` (87), sample 1/0,
  `DXGI_USAGE_RENDER_TARGET_OUTPUT`, buffer count **3**
  (`BUFFER_COUNT`, flip-sequential), flags 0. DComp mode:
  `CreateSwapChainForComposition`, `DXGI_SCALING_STRETCH`,
  `DXGI_ALPHA_MODE_PREMULTIPLIED`, plus the DirectComposition tree
  (`DCompositionCreateDevice(device.cast::<IDXGIDevice>())`,
  `CreateTargetForHwnd(hwnd, true)`, `CreateVisual`, `SetContent(chain)`,
  `SetRoot(visual)`, `Commit`). HWND mode: `CreateSwapChainForHwnd`,
  `DXGI_SCALING_NONE`, `DXGI_ALPHA_MODE_IGNORE`,
  `MakeWindowAssociation(hwnd, DXGI_MWA_NO_ALT_ENTER)`.
- **Clear + present** (`pre_draw`/`present`): `ClearRenderTargetView`
  with the caller color, then target/viewport binding in the pin's
  order, then `Present(0, DXGI_PRESENT(0))`.
- **Resize** (`resize`): unbind targets, drop views,
  `ResizeBuffers(3, w, h, BGRA8, 0)`, recreate views and viewport,
  rebind; unchanged extents no-op; extents clamp to >= 1.
- **Device loss** (`check_device_lost`): `GetDeviceRemovedReason()`;
  Ok = alive, Err(code) = removed (the code is the reason).
- **Driver identity** (`gpu_specs` + nvidia/amd/dxgi modules): vendor
  mapping 0x10DE/0x1002/0x8086/unknown, NvAPI
  (`nvapi_QueryInterface(0x2926aaad)`), AMD AGS, DXGI
  `CheckInterfaceSupport(&IDXGIDevice::IID)`, "Unknown Driver" fallback.

## Completion protocol (service-seam addition; NOT pinned behavior)

The pin has no per-submission retirement protocol — this is the
renderer contract's addition. `surface_present`:

1. `ClearRenderTargetView` (the frame's GPU work),
2. `Present(0, 0)` (submission/presentation, never completion),
3. `D3D11_QUERY_EVENT` **End after the last command**,
4. **one `Flush()`** so the marker is actually submitted — each
   submission here is the "rendering goes idle" case of the contract
   (there is no later frame that would carry the marker; the pin's own
   Flush point is its quiescing/device-lost path).

`submission_poll` reads the query with the RAW vtable HRESULT (the
windows-rs wrapper collapses S_OK and S_FALSE into `Ok(())`): `S_OK` →
completed; `S_FALSE` → pending; a negative code → the failure path with
the device-removal confirmation via `GetDeviceRemovedReason`.

Every accepted submission has exactly one terminal record. Retirement
of a pending submission is refused (`ERR_PENDING`). Retirement of a
failed submission is allowed only when a retire-time
`GetDeviceRemovedReason` confirms loss (terminal `ABORTED_LOSS`, the
documented release path); otherwise the record stays tracked
(`ERR_QUARANTINE` — quarantine, the service stays resident). A query
error alone is never a safe-abort proof. A Present error
(`ERR_PRESENT`) still returns a valid tracked token (the partial-submit
rule); polling resolves its real state.

## Service table

Reserved slot 2 holds the address of the static `#[repr(C)
GpuiGoRendererTable`: 21 function pointers then 15 self-check scalars —
`service_version` (6 since rev 16 ticket 53), `size_of_table` (224),
`align_of_table` (8), `size_of_surface_info`, `size_of_submission_state`,
`size_of_retire_record`, `size_of_device_info`,
`max_pending_submissions` (64), `max_instance_buffer_bytes` (256 MB,
the pin's instance-buffer ceiling), `path_multisample_count` (4, the
pin's MSAA path sample count), `size_of_debug_stats` (36, rev 16
ticket 53), `size_of_damage_rect` (16, version 6),
`max_damage_regions` (16, zgui's cap, version 6). The
Go loader copies the table out of DLL memory, validates every
size/alignment against its mirrors and rejects null function slots
before any call.

```c
i32 renderer_surface_create(u64 hwnd, u32 mode /* 0 dcomp-premultiplied, 1 hwnd-alpha-ignore */,
                            u32 width, u32 height /* device px, clamped >= 1 */,
                            u32 scale_bits /* f32 bits, recorded */, u64* out_surface);
i32 renderer_surface_resize(u64 surface, u32 width, u32 height);   // pinned resize path
i32 renderer_surface_info(u64 surface, GpuiGoSurfaceInfoRecord* out);
i32 renderer_surface_present(u64 surface, const u32 rgba_bits[4] /* f32 bits */, u64* out_submission);
i32 renderer_surface_destroy(u64 surface);                          // refused with ERR_PENDING while
                                                                    // un-retired submissions remain
i32 renderer_submission_poll(u64 submission, GpuiGoSubmissionStateRecord* out);
i32 renderer_submission_retire(u64 submission, GpuiGoRetireRecord* out); // exactly-once
i32 renderer_device_info(GpuiGoDeviceInfoRecord* out, u8* buf, u32 buf_len, u32* out_needed);
                                                                    // strings: capacity protocol
                                                                    // (ERR_CAPACITY + need)
i32 renderer_surface_draw_scene(u64 surface, u64 scene,           // ticket16: render + present +
                               u32 appearance /* 0 opaque, 1    // the tracked submission
                               transparent */, u64 atlas /* 0 = none,
                               required for sprite scenes */,
                               u64* out_submission);
i32 renderer_surface_render_scene(u64 surface, u64 scene,          // ticket16: render without present
                                  u32 appearance, u64 atlas);      // (the readback test path)
i32 renderer_surface_read_pixels(u64 surface, u8* out_bytes,       // ticket16: staging readback
                                 u32 capacity, u32* out_needed);    // (BGRA->RGBA swapped)
i32 renderer_surface_debug_stats(u64 surface,                      // version 5, rev 16: the group
                                 GpuiGoDebugStatsRecord* out);     // target pool observability
i32 renderer_surface_draw_scene_damage(u64 surface, u64 scene,    // version 6, rev 16 ticket 53:
    u32 appearance, u64 atlas,                                     // the retained-frame damage
    const GpuiGoRectRecord* rects, u32 count,                      // redraw + Present1
    u32 flags /* bit 0 force full */, u64* out_submission);
i32 renderer_surface_read_frame_pixels(u64 surface, u8* out_bytes, // version 6: the retained
    u32 capacity, u32* out_needed);                                // target's readback (test seam)
```

`GpuiGoDebugStatsRecord` (36 bytes, alignment 4; the ticket-53 fields
appended after `record_size`):
`pool_target_count` (the retained group targets), `pool_bytes` (the
committed bytes, targets plus their half-res blur scratch),
`pool_overflow_count` (the CUMULATIVE count of groups the budget
demoted to inline - never reset), `record_size`, `state_bind_count`
(the §3.12 tracker's cumulative issued binds, device-global),
`tess_cache_hits` (the scene service's global counter),
`damage_fraction` (the last frame's merged damage area over the
window area, per-mille), `full_present_count`,
`partial_present_count` (the damage entry's cumulative presents).

### The scene draw entries (ticket16)

`renderer_surface_draw_scene` is the pin's `DirectXRenderer::draw`:
render the finished scene (resolved through the scene service's
`scene_render_snapshot`) into the surface's back buffer, `Present(0,
0)`, then the ticket07 event-query retirement protocol. The draw walks
the scene's compiled plan command stream — quads and shadows (ordinary
and smoothed), underlines, the two-pass MSAA path rasterization/sprite
pair, monochrome/subpixel/polychrome sprites (atlas textures resolved
through the atlas service's `texture_srv`, the pin's
`get_texture_view`), the blur/offscreen chain of backdrop filters and
content-filter groups (downsample + separable gaussian ping-pong +
composite, the offscreen scene target, isolated group targets per
nesting depth, inline groups, the final `dx_blit`), all built from the
pinned `gpui_ce_render` build-time DXBC artifacts — the maintainer
build compiles the shaders (wgsl-rs → Naga validation → HLSL sm_5.0 →
`D3DCompile` → embedded DXBC) and consumers never run the shader
toolchain.

`appearance` selects the pin's clear color (0 = opaque → `[1,1,1,1]`,
1 = transparent → `[0,0,0,0]`). `atlas` is the atlas handle that owns
the sprite textures (0 is allowed only for scenes without sprite
batches; a sprite batch with a null atlas is `ERR_BAD_HANDLE`).

Honest boundary: surface *primitives* follow the pinned import error
path — the ABI's surface source is the pinned
`SurfaceSource::Unsupported` stand-in and the pinned `draw_surfaces`
bails on it, so a surface batch returns `ERR_UNSUPPORTED_SOURCE`
(-11); pixel output for imported surface sources awaits the
capture-producer slice (the pinned import path requires
`SurfaceSource::WindowsCapture` frames). A scene handle that does not
resolve to a finished, healthy scene returns `ERR_SCENE` (-12).

## Records

- `GpuiGoSurfaceInfoRecord` (36 bytes, align 4): mode, format (87),
  buffer_count (3), width, height, feature_level (raw
  `D3D_FEATURE_LEVEL`), resize_generation, scale_bits, record_size.
- `GpuiGoSubmissionStateRecord` (20 bytes, align 4): state (0 pending /
  1 completed / 2 failed), device_removed_reason (i32; only when loss
  was confirmed), data_hr (raw last GetData HRESULT), present_hr
  (HRESULT at acceptance; non-zero marks the partial-submit path),
  record_size.
- `GpuiGoRetireRecord` (20 bytes, align 4): terminal (0 completed, 1
  aborted-on-confirmed-loss), device_removed_reason, reserved[2],
  record_size.
- `GpuiGoDeviceInfoRecord` (64 bytes, align 8): vendor_id, device_id,
  feature_level, adapter_flags (bit 0 = software), reserved,
  dedicated_video_memory (u64), multithread_protected, then three
  offset/length pairs into the caller's byte buffer: adapter
  description, driver name, driver version (UTF-8, packed in that
  order).

## Status codes

| Value | Name | Meaning |
|---|---|---|
| 0 | `ERR_OK` | success |
| -1 | `ERR_STALE_HANDLE` | handle encoded a past generation (destroyed/retired) |
| -2 | `ERR_BAD_HANDLE` | handle malformed or never issued |
| -3 | `ERR_NULL_ARG` | required pointer argument was null |
| -4 | `ERR_BAD_VALUE` | invalid mode/extent/record value |
| -5 | `ERR_DEVICE` | shared device failure (no compatible adapter, etc.) |
| -6 | `ERR_BUSY` | per-surface pending-submission cap (64) exceeded; backpressure before GPU work |
| -7 | `ERR_PENDING` | un-retired submissions remain (retire of pending, destroy outstanding) |
| -8 | `ERR_QUARANTINE` | retirement refused: failed without confirmed device loss |
| -9 | `ERR_PRESENT` | Present failed; **not a caller error**: the token in `out_submission` is valid and tracked (partial submit) |
| -10 | `ERR_CAPACITY` | device-info string buffer too small (`out_needed`), or `surface_read_pixels` capacity (ticket16) |
| -11 | `ERR_UNSUPPORTED_SOURCE` | the scene's surface source is the pinned `SurfaceSource::Unsupported` stand-in; the pinned draw path bails on it (ticket16) |
| -12 | `ERR_SCENE` | the draw entry's scene handle did not resolve to a finished, healthy scene (stale, malformed, unfinished or failed) (ticket16) |
| 101 | `ERR_PANIC` | native panic contained during this call |

## Bounded pending submissions

64 tracked (un-retired) submissions per surface; the 65th present is
rejected with `ERR_BUSY` before any GPU work is accepted. The cap is
mirrored in the table's `max_pending_submissions` and enforced Go-side
before native entry (typed backpressure). Queries are reused only after
their previous result retired — each submission owns its query object
until retirement releases it.
