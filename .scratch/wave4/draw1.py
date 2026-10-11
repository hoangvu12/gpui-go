import sys
p = 'reference/native/src/renderer/draw.rs'
s = open(p, encoding='utf-8').read()

def rep(old, new, count=1):
    global s
    assert s.count(old) == count, f"needle count {s.count(old)} != {count}: {old[:70]!r}"
    s = s.replace(old, new)

# 1) imports
rep('''use gpui_render::{
    InstanceRange,
    artifacts::{Dx11DrawConstants, Dx11Shader, NATIVE_SHADERS},
    blur::{
        BlurAxis, BlurKernel, BlurUniforms, FilterCompositeClip,
        GAUSSIAN_CUTOFF_STANDARD_DEVIATIONS, downsampled_dimension,
    },
    path_types::{PathRasterizationVertex, PathSprite},
    shaders::{
        common::{FontRasterizationUniforms, GlobalUniforms},
        interface as shader_interface,
        surface::SurfaceUniforms,
    },
};''',
'''use gpui_render::{
    InstanceRange,
    artifacts::{Dx11DrawConstants, Dx11Shader, NATIVE_SHADERS},
    blur::{
        BlurAxis, BlurKernel, BlurUniforms, FilterCompositeClip,
        GAUSSIAN_CUTOFF_STANDARD_DEVIATIONS, downsampled_dimension,
    },
    path_types::{PathRasterizationVertex, PathSprite},
    shaders::{
        common::{
            Bounds as ShaderBounds, ContentMask as ShaderContentMask,
            Edges as ShaderEdges, FontRasterizationUniforms, GlobalUniforms,
            SurfaceColorFormat,
        },
        interface as shader_interface,
        surface::SurfaceUniforms,
    },
};''')

rep('''use crate::scene::{
    SceneRenderSnapshot, SceneRenderSnapshot as Snapshot, batch_primitive_kind, command_kind,
    scene_render_snapshot,
};''',
'''use crate::scene::{
    GpuiGoSceneSurfaceRecord, SceneRenderSnapshot, SceneRenderSnapshot as Snapshot,
    batch_primitive_kind, command_kind, scene_render_snapshot, surface_source_kind,
};''')

# 2) DrawGlobal: surface_views
rep('''pub(super) struct DrawGlobal {
    globals: GlobalElements,
    pipelines: DrawPipelines,
}''',
'''pub(super) struct DrawGlobal {
    globals: GlobalElements,
    pipelines: DrawPipelines,
    /// Ticket28: the capture surface view cache — the pin's
    /// `surface_views: FxHashMap<usize, CachedSurfaceView>` keyed by
    /// the texture's raw pointer (the COM identity). The cache is
    /// device-global (dropped wholesale by a device rebuild —
    /// `clear_surface_views`), and retained per drawn scene (the pin's
    /// `retain_surface_views`).
    pub(super) surface_views: std::collections::HashMap<usize, CachedSurfaceView>,
}

/// Ticket28: the pin's `CachedSurfaceView` — an owned texture clone
/// (keeping the frame's destination texture alive while the view is
/// cached) plus its shader-resource view on this device.
pub(super) struct CachedSurfaceView {
    pub(super) texture: ID3D11Texture2D,
    pub(super) srv: ID3D11ShaderResourceView,
}''')

# 2b) DrawGlobal construction site
rep('''    state.draw = Some(DrawGlobal {
        globals,
        pipelines,
    });''',
'''    state.draw = Some(DrawGlobal {
        globals,
        pipelines,
        surface_views: std::collections::HashMap::new(),
    });''')

# 3) the batch call site
rep('''                batch_primitive_kind::SURFACES => {
                    draw_surfaces(
                        &snapshot.surfaces[range.clone()],
                    )?;''',
'''                batch_primitive_kind::SURFACES => {
                    draw_surfaces(
                        state,
                        draw,
                        &frame,
                        viewport,
                        &snapshot.surfaces[range.clone()],
                        &snapshot.surface_opacities[range.clone()],
                    )?;''')

# 4) the real draw_surfaces
rep('''/// The pin's `draw_surfaces` import path: the pinned renderer imports
/// exactly one source kind (`SurfaceSource::WindowsCapture`); the ABI
/// surface record carries the pinned `SurfaceSource::Unsupported`
/// stand-in, so a surface batch follows the pinned *error* path
/// (`bail!("unsupported surface source")`) — explicit, not a blank
/// successful draw. Pixel output for imported surface sources awaits
/// the capture-producer slice.
fn draw_surfaces(surfaces: &[gpui::PaintSurface]) -> Result<(), i32> {
    if surfaces.is_empty() {
        return Ok(());
    }
    Err(renderer_status::ERR_UNSUPPORTED_SOURCE)
}''',
'''/// The pin's `draw_surfaces` (directx_renderer.rs 1174-1248): one draw
/// per surface with per-draw uniforms. The pinned renderer imports
/// exactly one source kind (`SurfaceSource::WindowsCapture`); the ABI
/// surface record carries tag 1 = a capture frame handle (resolved
/// through the capture service's frame registry under the renderer
/// lock — the allowed renderer->capture nesting) or the pinned
/// `SurfaceSource::Unsupported` stand-in (tag 0), which draws nothing
/// and fails the whole draw with the pinned import error ("DirectX
/// renderer cannot import this surface source" →
/// `renderer_status::ERR_UNSUPPORTED_SOURCE`).
#[allow(clippy::too_many_arguments)]
fn draw_surfaces(
    state: &mut RendererState,
    draw: &mut DrawGlobal,
    frame: &FrameBindings<'_>,
    viewport: &D3D11_VIEWPORT,
    surfaces: &[GpuiGoSceneSurfaceRecord],
    opacities: &[f32],
) -> Result<(), i32> {
    if surfaces.is_empty() {
        return Ok(());
    }
    let device = state
        .device
        .as_ref()
        .ok_or(renderer_status::ERR_DEVICE)?
        .device
        .clone();
    let ctx = frame.device_context;
    let surface_cb = [Some(draw.pipelines.surfaces.params_buffer.clone())];
    let sampler = [Some(draw.globals.sampler.clone())];

    for (index, surface) in surfaces.iter().enumerate() {
        if surface.source_tag != surface_source_kind::CAPTURE_FRAME {
            // The pin's Unsupported import bail (the whole draw fails —
            // an explicit error, never a silent blank surface).
            return Err(renderer_status::ERR_UNSUPPORTED_SOURCE);
        }
        let frame_handle = surface.source_reserved[0] as u64;
        // Resolve the frame's texture through the capture service (an
        // owned clone; the lock order is renderer -> capture).
        let (texture, _frame_width, _frame_height) =
            crate::capture::frame_texture_for_draw(frame_handle)?;
        let key = Interface::as_raw(&texture) as usize;
        if !draw.surface_views.contains_key(&key) {
            let mut srv: Option<ID3D11ShaderResourceView> = None;
            unsafe {
                device
                    .CreateShaderResourceView(&texture, None, Some(&mut srv))
                    .map_err(|e| e.code().0)?;
            }
            let Some(srv) = srv else {
                return Err(renderer_status::ERR_DEVICE);
            };
            draw.surface_views
                .insert(key, CachedSurfaceView { texture: texture.clone(), srv });
        }
        let texture_srv = &draw
            .surface_views
            .get(&key)
            .ok_or(renderer_status::ERR_DEVICE)?
            .srv;
        // The surface shader declares both planes; RGBA captures bind
        // one view to both (the pin's comment).
        let texture_srvs = [Some(texture_srv.clone()), Some(texture_srv.clone())];

        let bounds = surface.bounds;
        let mask = surface.content_mask;
        let uniforms = SurfaceUniforms {
            bounds: ShaderBounds {
                origin: vec2f(f32::from_bits(bounds.x), f32::from_bits(bounds.y)),
                size: vec2f(f32::from_bits(bounds.w), f32::from_bits(bounds.h)),
            },
            content_mask: ShaderContentMask {
                bounds: ShaderBounds {
                    origin: vec2f(f32::from_bits(mask.x), f32::from_bits(mask.y)),
                    size: vec2f(f32::from_bits(mask.w), f32::from_bits(mask.h)),
                },
                // The scene ABI carries hard masks (no fade edges), so
                // the shader mask's fade is zero — the same value the
                // ABI's other masked primitives use.
                fade_out: ShaderEdges {
                    top: 0.0,
                    right: 0.0,
                    bottom: 0.0,
                    left: 0.0,
                },
            },
            color_format: SurfaceColorFormat::Rgba,
            opacity: opacities.get(index).copied().unwrap_or(1.0),
            padding0: 0,
            padding1: 0,
            padding2: 0,
            padding3: 0,
            padding4: 0,
            padding5: 0,
        };
        update_buffer(ctx, &draw.pipelines.surfaces.params_buffer, &[uniforms])?;

        unsafe {
            ctx.IASetPrimitiveTopology(D3D_PRIMITIVE_TOPOLOGY_TRIANGLESTRIP);
            ctx.RSSetViewports(Some(std::slice::from_ref(viewport)));
            ctx.VSSetShader(&draw.pipelines.surfaces.vertex, None);
            ctx.PSSetShader(&draw.pipelines.surfaces.fragment, None);
            let cbuffers = frame.globals.cbuffers();
            ctx.VSSetConstantBuffers(0, Some(&cbuffers));
            ctx.PSSetConstantBuffers(0, Some(&cbuffers));
            ctx.VSSetConstantBuffers(DATA_REGISTER, Some(&surface_cb));
            ctx.PSSetConstantBuffers(DATA_REGISTER, Some(&surface_cb));
            ctx.PSSetShaderResources(PRIMARY_TEXTURE_REGISTER, Some(&texture_srvs));
            ctx.PSSetSamplers(SURFACE_SAMPLER_REGISTER, Some(&sampler));
            ctx.OMSetBlendState(&draw.pipelines.surfaces.blend, None, 0xFFFFFFFF);
            ctx.DrawInstanced(4, 1, 0, 0);
        }
    }
    Ok(())
}

/// The pin's `retain_surface_views` (directx_renderer.rs 1599-1613):
/// after a scene's draw, keep only the views whose textures the scene
/// still references; the rest drop (the frame textures the caller
/// released die with their last reference).
fn retain_surface_views(draw: &mut DrawGlobal, surfaces: &[GpuiGoSceneSurfaceRecord]) {
    let active_keys: std::collections::HashSet<usize> = surfaces
        .iter()
        .filter(|surface| surface.source_tag == surface_source_kind::CAPTURE_FRAME)
        .map(|surface| surface.source_reserved[0] as u64)
        .collect();
    // Key by the frame handle's resolved texture pointer: resolve once
    // (the capture registry is under its own lock; renderer->capture
    // nesting is allowed here because the draw path already holds the
    // renderer lock... this retention runs OUTSIDE the renderer lock in
    // the render flow's tail, so the capture lock alone is taken).
    let mut resolved: std::collections::HashMap<u64, usize> =
        std::collections::HashMap::new();
    for handle in &active_keys {
        if let Ok((texture, _, _)) = crate::capture::frame_texture_for_draw(*handle) {
            resolved.insert(*handle, Interface::as_raw(&texture) as usize);
        }
    }
    let keep: std::collections::HashSet<usize> =
        resolved.values().copied().collect();
    draw.surface_views.retain(|key, _| keep.contains(key));
}

/// Ticket29: drops every cached capture surface view (the old
/// device's SRVs) — called by the device rebuild. Returns the count.
pub(super) fn clear_surface_views(state: &mut RendererState) -> u32 {
    // The DrawGlobal is lazily recreated; the views live inside it.
    match state.draw.as_mut() {
        Some(draw) => {
            let count = draw.surface_views.len() as u32;
            draw.surface_views.clear();
            count
        }
        None => 0,
    }
}''')

# 5) the retention call after the command loop (before the offscreen blit)
rep('''    // Present the offscreen scene by blitting it into the swapchain.
    if use_offscreen {
        dx_blit(''',
'''    // The pin's retain_surface_views: after the command loop, keep
    // only the capture views this scene still references.
    retain_surface_views(draw, &snapshot.surfaces);

    // Present the offscreen scene by blitting it into the swapchain.
    if use_offscreen {
        dx_blit(''')

# 6) the recovery gate in render_scene_entry
rep('''    let mut state = lock_global();
    ensure_device(&mut state)?;
    ensure_draw_global(&mut state)?;
    let mut lease = DrawLease::take(&mut state)?;''',
'''    let mut state = lock_global();
    // Ticket29: the recovery gate (the pin's skip_draws in `draw` —
    // the skip is observable here instead of silent).
    if state.recovering {
        return Err(renderer_status::ERR_RECOVERING);
    }
    ensure_device(&mut state)?;
    ensure_draw_global(&mut state)?;
    let mut lease = DrawLease::take(&mut state)?;''')

# 7) the surfaces pipeline is no longer dead code
rep('''    /// Constructed like the pin's; the draw path takes the pinned
    /// import-error branch for the ABI's Unsupported surface sources
    /// before binding it.
    #[expect(dead_code)]
    surfaces: SurfacePipeline,''',
'''    /// Constructed like the pin's; the surface draw path binds it per
    /// imported capture surface (ticket28).
    surfaces: SurfacePipeline,''')

# 8) surface_render_to_image_body, appended near surface_read_pixels_body
rep('''/// `renderer_surface_read_pixels`: staging readback of the surface's
/// current back buffer (the pin's `render_to_image` tail: staging
/// texture, `CopyResource`, `Map(READ)`, row copy, BGRA->RGBA swap).''',
'''/// `renderer_surface_render_to_image` (ticket32): the pinned
/// test-support path VERBATIM (directx_renderer.rs 788-860): render the
/// scene WITHOUT presenting (the pin's `render`), then stage-copy the
/// CURRENT render target into owned RGBA bytes (staging texture with
/// the source desc, `CopyResource`, `Map(READ)`, the row-pitch copy,
/// the BGRA->RGBA swap), dimensions out — and the pin's
/// device-recovery refusal FIRST ("render_to_image unavailable while
/// recovering from a lost device").
pub(super) fn surface_render_to_image_body(
    surface: u64,
    scene: u64,
    appearance: u32,
    atlas: u64,
    out_bytes: *mut u8,
    capacity: u32,
    out_needed: *mut u32,
) -> i32 {
    if out_needed.is_null() {
        return renderer_status::ERR_NULL_ARG;
    }
    let result = catch_unwind(AssertUnwindSafe(|| {
        // The pin's ensure!( !self.skip_draws, ...): the recovery
        // refusal fires before any GPU work.
        {
            let state = lock_global();
            if state.recovering {
                return Ok(renderer_status::ERR_RECOVERING);
            }
        }
        // Render without presenting (the pin's `render` call — the
        // readback test path).
        render_scene_entry(surface, scene, appearance, atlas)?;
        // The readback tail (the pin's render_to_image body).
        let (slot_index, slot_generation) = super::surface_handle_parts(surface);
        let mut state = lock_global();
        let device = ensure_device(&mut state)?;
        let surface_state = resolve_slot_mut(&mut state, slot_index, slot_generation)?;
        let render_target = surface_state
            .render_target
            .as_ref()
            .ok_or(renderer_status::ERR_DEVICE)?
            .clone();

        let mut source_desc = D3D11_TEXTURE2D_DESC::default();
        unsafe { render_target.GetDesc(&mut source_desc) };
        let width = source_desc.Width;
        let height = source_desc.Height;
        let row_bytes = width as usize * 4;
        let needed = (row_bytes * height as usize) as u32;
        unsafe { *out_needed = needed };
        if needed > capacity {
            return Ok(renderer_status::ERR_CAPACITY);
        }
        if capacity > 0 && out_bytes.is_null() {
            return Ok(renderer_status::ERR_NULL_ARG);
        }

        let staging_desc = D3D11_TEXTURE2D_DESC {
            Usage: D3D11_USAGE_STAGING,
            BindFlags: 0,
            CPUAccessFlags: D3D11_CPU_ACCESS_READ.0 as u32,
            MiscFlags: 0,
            MipLevels: 1,
            ArraySize: 1,
            SampleDesc: DXGI_SAMPLE_DESC {
                Count: 1,
                Quality: 0,
            },
            ..source_desc
        };
        let mut staging = None;
        unsafe {
            device
                .device
                .CreateTexture2D(&staging_desc, None, Some(&mut staging))
                .map_err(|e| e.code().0)?;
        }
        let staging = staging.ok_or(renderer_status::ERR_DEVICE)?;
        unsafe {
            device.context.CopyResource(&staging, &render_target);
        }

        let mut mapped = D3D11_MAPPED_SUBRESOURCE::default();
        unsafe {
            device
                .context
                .Map(&staging, 0, D3D11_MAP_READ, 0, Some(&mut mapped))
                .map_err(|e| e.code().0)?;
        }
        let mut pixels = vec![0u8; row_bytes * height as usize];
        // SAFETY: a successful `Map` exposes `RowPitch * height`
        // readable bytes until `Unmap`; each destination row is
        // disjoint within the exactly-sized output allocation.
        unsafe {
            let source = mapped.pData.cast::<u8>();
            for row in 0..height as usize {
                std::ptr::copy_nonoverlapping(
                    source.add(row * mapped.RowPitch as usize),
                    pixels.as_mut_ptr().add(row * row_bytes),
                    row_bytes,
                );
            }
            device.context.Unmap(&staging, 0);
        }
        for pixel in pixels.chunks_exact_mut(4) {
            pixel.swap(0, 2);
        }
        unsafe {
            let out = std::slice::from_raw_parts_mut(out_bytes, needed as usize);
            out.copy_from_slice(&pixels);
        }
        Ok(renderer_status::OK)
    }));
    match result {
        Ok(Ok(code)) => code,
        Ok(Err(code)) => code,
        Err(payload) => {
            drop(payload);
            renderer_status::ERR_PANIC
        }
    }
}

/// `renderer_surface_read_pixels`: staging readback of the surface's
/// current back buffer (the pin's `render_to_image` tail: staging
/// texture, `CopyResource`, `Map(READ)`, row copy, BGRA->RGBA swap).''')

open(p, 'w', encoding='utf-8', newline='\n').write(s)
print("draw.rs done")
