//! The scene-drawing half of the renderer service (ticket16).
//!
//! A port of the pinned gpui-CE Windows renderer's draw path
//! (`crates/gpui_windows/src/directx_renderer.rs` at
//! `254b5dbd47cbb5acbcc5bbdcbb322a339276c88a`): the build-time DXBC
//! shader artifacts of the pinned `gpui_render` crate
//! (`gpui_render::artifacts::NATIVE_SHADERS` — exactly what the pin's
//! `shader_resources::ShaderModule` resolves), the whole-frame instance
//! buffers, the fixed pipeline states and blend states, the path MSAA
//! intermediate, the blur/offscreen targets of the filter chain, and
//! the render-command walk (`DirectXRenderer::render`).
//!
//! # What is drawn
//!
//! * Quads and shadows (the ordinary and smoothed variants),
//!   underlines — the `Instances` pipelines;
//! * Paths — the two-pass MSAA rasterization/sprite path of the pin
//!   (`draw_paths_to_intermediate` + `draw_paths_from_intermediate`,
//!   `PATH_MULTISAMPLE_COUNT = 4`);
//! * Monochrome/subpixel/polychrome sprites — atlas textures resolved
//!   through the atlas service's `texture_srv` (the pin's
//!   `get_texture_view`);
//! * Surfaces — the `SURFACES` per-draw-uniform pipeline. The pinned
//!   `draw_surfaces` imports exactly one source kind
//! (`SurfaceSource::WindowsCapture`); the ABI surface record carries
//! the pinned `SurfaceSource::Unsupported` stand-in, so a surface batch
//! draws through the pinned *error* path
//!   (`renderer_status::ERR_UNSUPPORTED_SOURCE`); pixel output for
//!   imported surface sources awaits the capture-producer slice;
//! * Backdrop filters and content-filter groups — the pinned blur
//!   chain (`dx_blur_and_composite`: downsample + separable gaussian
//!   ping-pong + composite), the offscreen scene target, the isolated
//!   group targets (one per nesting depth up to
//!   `MAX_FILTER_GROUP_DEPTH`, deeper groups inline) and the final
//!   `dx_blit`.
//!
//! # Adaptations (each documented, none silent)
//!
//! * The pin creates one pipeline set per renderer (per window). This
//!   service shares one D3D11 device across surfaces and serializes
//!   every immediate-context call behind the renderer-global mutex, so
//!   the shader/blend objects and the global constant buffers live on
//!   the shared device (lazily created at first draw) and only the
//!   size-dependent path/blur targets are per-surface. The buffers are
//!   per-draw rewritten (`MAP_WRITE_DISCARD`), which makes them
//!   equivalent to the pin's per-renderer buffers under serialization.
//! * The `ID3D11Multithread` protection is already set on the shared
//!   device (ticket07), which the renderer contract requires before the
//!   device is shared with capture; the pin sets it in the same spirit.
//! * The font rasterization uniforms come from the pin's
//!   `get_font_info` (DirectWrite rendering parameters, queried once).
//! * Completion: the draw entries reuse the ticket07 event-query
//!   retirement protocol (a `D3D11_QUERY_EVENT` ended after the last
//!   command, one explicit `Flush`, `GetData` polling) — the contract's
//!   added service-seam protocol, not pinned behavior.

use std::cell::Cell;
use std::panic::{catch_unwind, AssertUnwindSafe};
use std::rc::Rc;
use std::slice;
use std::sync::OnceLock;
use std::sync::atomic::{AtomicUsize, Ordering};

use gpui_render::{
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
};
use wgsl_rs::std::{vec2f, vec4f};
use windows::core::Interface;
use windows::Win32::Graphics::Direct3D::*;
use windows::Win32::Graphics::Direct3D11::*;
use windows::Win32::Graphics::DirectWrite::{
    DWRITE_FACTORY_TYPE_SHARED, DWRITE_PIXEL_GEOMETRY_BGR, DWriteCreateFactory, IDWriteFactory5,
    IDWriteRenderingParams1,
};
use windows::Win32::Graphics::Dxgi::Common::*;
use windows::Win32::Graphics::Dxgi::DXGI_PRESENT;

use crate::scene::{
    GpuiGoSceneCommandRecord, GpuiGoSceneSurfaceRecord, SceneRenderSnapshot,
    SceneRenderSnapshot as Snapshot, batch_primitive_kind, command_kind, scene_render_snapshot,
    snapshot_bounds, snapshot_radii, surface_source_kind,
};

use super::{
    RendererState, SurfaceState, ensure_device, lock_global, renderer_status, resolve_slot_mut,
};

// ---------------------------------------------------------------------------
// Constants (the pin's)
// ---------------------------------------------------------------------------

/// MSAA sample count of the path rasterization target
/// (`PATH_MULTISAMPLE_COUNT`): 4x MSAA, guaranteed supported by D3D11.
pub const PATH_MULTISAMPLE_COUNT: u32 = 4;

/// The pin's instance-buffer size ceiling (`MAX_INSTANCE_BUFFER_SIZE`).
pub const MAX_INSTANCE_BUFFER_SIZE: usize = 256 * 1024 * 1024;

// ---------------------------------------------------------------------------
// The R16 group target pool (browser-core ticket 50)
// ---------------------------------------------------------------------------

/// Pool cap: the maximum number of retained group targets per surface
/// (the R16 design's budget; over it a group renders inline and the
/// overflow is observable - a debug-stats counter and a debug-build
/// warning, never a crash, per the user's 2026-10-24 decision).
pub const GROUP_TARGET_POOL_MAX_COUNT: usize = 64;
/// Pool cap: the maximum committed bytes of group targets (including
/// their half-resolution blur scratch) per surface.
pub const GROUP_TARGET_POOL_MAX_BYTES: u64 = 256 * 1024 * 1024;
/// The pool's bucket granularity: group targets are sized in 256-px
/// buckets (clamped to the window), so sibling groups of nearby sizes
/// reuse one backing.
pub const GROUP_TARGET_BUCKET_GRANULARITY: u32 = 256;

/// One pooled offscreen group target (bounds-sized, bucketed). The
/// half-resolution pair is created lazily when a FILTER group's blur
/// chain runs against this entry (an opacity or clip group never
/// blurs, so most entries carry none).
struct GroupTargetEntry {
    rtv: Option<ID3D11RenderTargetView>,
    srv: Option<ID3D11ShaderResourceView>,
    width: u32,
    height: u32,
    half_a_rtv: Option<ID3D11RenderTargetView>,
    half_a_srv: Option<ID3D11ShaderResourceView>,
    half_b_rtv: Option<ID3D11RenderTargetView>,
    half_b_srv: Option<ID3D11ShaderResourceView>,
    /// Currently acquired by an open group (freed at the group's end).
    in_use: bool,
    /// Acquired at least once THIS frame (viz's UpdateRenderPassTextures
    /// rule: the frame-end prune keeps the backings a group used this
    /// frame, released or not, and drops the stale ones).
    frame_used: bool,
}

impl GroupTargetEntry {
    fn bytes(&self) -> u64 {
        let full = self.width as u64 * self.height as u64 * 4;
        let half = if self.half_a_rtv.is_some() {
            (self.width as u64 / 2 + 1) * (self.height as u64 / 2 + 1) * 4 * 2
        } else {
            0
        };
        full + half
    }
}

/// The bounds-sized, bucketed offscreen group-target pool (rev 16,
/// ticket 50): allocated lazily, kept across frames while groups keep
/// using them, dropped when unused or too small - viz's render-pass
/// backing model (`UpdateRenderPassTextures`: keep the backings still
/// in frame, delete the stale or insufficient ones). Over the caps a
/// group renders INLINE and the overflow is observable: the cumulative
/// `overflow_count` (the debug-stats counter) plus a warning in debug
/// builds. Never a crash, never a failed frame.
#[derive(Default)]
pub(super) struct GroupTargetPool {
    entries: Vec<GroupTargetEntry>,
    overflow_count: u32,
}

/// One acquired pool target (the render walk's lease). `origin` is the
/// bucket's window-space origin; `size` its extent - children render
/// into the bucket through the offset viewport, and the composite
/// samples it region-relative ((window position - origin) / size).
#[derive(Clone)]
struct GroupLease {
    rtv: Option<ID3D11RenderTargetView>,
    srv: Option<ID3D11ShaderResourceView>,
    half_a_rtv: Option<ID3D11RenderTargetView>,
    half_a_srv: Option<ID3D11ShaderResourceView>,
    half_b_rtv: Option<ID3D11RenderTargetView>,
    half_b_srv: Option<ID3D11ShaderResourceView>,
    origin: [f32; 2],
    size: [f32; 2],
}

impl GroupTargetPool {
    /// The pool's debug-stats snapshot: retained target count, the
    /// committed bytes and the cumulative overflow counter (the
    /// renderer service's `surface_debug_stats` entry reads it).
    pub(super) fn stats(&self) -> (u32, u32, u32) {
        let bytes: u64 = self.entries.iter().map(|entry| entry.bytes()).sum();
        (
            self.entries.len() as u32,
            u32::try_from(bytes.min(u32::MAX as u64)).unwrap_or(u32::MAX),
            self.overflow_count,
        )
    }

    /// Acquires a bucket for the given window-space region (origin +
    /// extent), creating or reusing a backing under the pool caps.
    /// `needs_half` pre-creates the half-resolution blur scratch (filter
    /// groups). Returns None over the caps - the caller renders the
    /// group inline and the overflow stays observable.
    ///
    /// The bucket is bounds-sized (ticket 51 finishing 50's handed-off
    /// gap): the region's extent rounded UP to the 256-px granularity
    /// and clamped to the window, its origin the region's floor clamped
    /// so the bucket stays inside the window. Sibling groups of nearby
    /// sizes reuse one backing (the quantization), and a group far
    /// smaller than the window no longer pays the window's bytes
    /// (risk 1's measured mitigation).
    fn acquire(
        &mut self,
        device: &ID3D11Device,
        region_origin: [f32; 2],
        region_size: [f32; 2],
        window: (u32, u32),
        needs_half: bool,
    ) -> Option<GroupLease> {
        // The bucket size: the region's extent quantized up to the
        // 256-px granularity, clamped to the window (the bucket never
        // escapes the surface; the offset viewport drops what falls
        // outside it, exactly the content the composite never draws).
        let granularity = GROUP_TARGET_BUCKET_GRANULARITY as f32;
        let quantize = |extent: f32, window_extent: u32| -> u32 {
            let clamped_extent = if !extent.is_finite() || extent <= 0.0 {
                1.0
            } else {
                extent
            };
            let bucket = (clamped_extent / granularity).ceil() * granularity;
            (bucket.min(window_extent.max(1) as f32).max(1.0)) as u32
        };
        let width = quantize(region_size[0], window.0);
        let height = quantize(region_size[1], window.1);
        // The bucket's origin: the region's floor, clamped so the
        // bucket stays inside the window (the composite region is
        // window-space either way).
        let origin = [
            region_origin[0]
                .floor()
                .max(0.0)
                .min((window.0 as f32 - width as f32).max(0.0)),
            region_origin[1]
                .floor()
                .max(0.0)
                .min((window.1 as f32 - height as f32).max(0.0)),
        ];
        // Reuse a free entry of the exact bucket size.
        if let Some(position) = self
            .entries
            .iter()
            .position(|entry| !entry.in_use && entry.width == width && entry.height == height)
        {
            let entry = &mut self.entries[position];
            entry.in_use = true;
            entry.frame_used = true;
            if needs_half && entry.half_a_rtv.is_none() {
                let half_w = downsampled_dimension(width);
                let half_h = downsampled_dimension(height);
                let (_, half_a_rtv, half_a_srv) =
                    create_color_target(device, half_w, half_h).map_err(|_| ()).ok()?;
                let (_, half_b_rtv, half_b_srv) =
                    create_color_target(device, half_w, half_h).map_err(|_| ()).ok()?;
                entry.half_a_rtv = half_a_rtv;
                entry.half_a_srv = half_a_srv;
                entry.half_b_rtv = half_b_rtv;
                entry.half_b_srv = half_b_srv;
            }
            let entry = &self.entries[position];
            return Some(GroupLease {
                rtv: entry.rtv.clone(),
                srv: entry.srv.clone(),
                half_a_rtv: entry.half_a_rtv.clone(),
                half_a_srv: entry.half_a_srv.clone(),
                half_b_rtv: entry.half_b_rtv.clone(),
                half_b_srv: entry.half_b_srv.clone(),
                origin,
                size: [width as f32, height as f32],
            });
        }
        // Create a new entry under the caps - or replace a free one
        // (too small or too large) when the count cap is already met.
        let (_, rtv, srv) = create_color_target(device, width, height)
            .map_err(|_| ())
            .ok()?;
        let half_bytes = if needs_half {
            (downsampled_dimension(width) as u64) * (downsampled_dimension(height) as u64) * 4 * 2
        } else {
            0
        };
        let new_bytes = (width as u64 * height as u64 * 4) + half_bytes;
        let make_half = || -> Option<(
            Option<ID3D11RenderTargetView>,
            Option<ID3D11ShaderResourceView>,
            Option<ID3D11RenderTargetView>,
            Option<ID3D11ShaderResourceView>,
        )> {
            if !needs_half {
                return None;
            }
            let half_w = downsampled_dimension(width);
            let half_h = downsampled_dimension(height);
            let (_, half_a_rtv, half_a_srv) =
                create_color_target(device, half_w, half_h).map_err(|_| ()).ok()?;
            let (_, half_b_rtv, half_b_srv) =
                create_color_target(device, half_w, half_h).map_err(|_| ()).ok()?;
            Some((half_a_rtv, half_a_srv, half_b_rtv, half_b_srv))
        };
        let half_fields = |pair: Option<(
            Option<ID3D11RenderTargetView>,
            Option<ID3D11ShaderResourceView>,
            Option<ID3D11RenderTargetView>,
            Option<ID3D11ShaderResourceView>,
        )>| -> (
            Option<ID3D11RenderTargetView>,
            Option<ID3D11ShaderResourceView>,
            Option<ID3D11RenderTargetView>,
            Option<ID3D11ShaderResourceView>,
        ) {
            match pair {
                Some((a_rtv, a_srv, b_rtv, b_srv)) => (a_rtv, a_srv, b_rtv, b_srv),
                None => (None, None, None, None),
            }
        };
        let held_bytes: u64 = self.entries.iter().map(|entry| entry.bytes()).sum();
        if self.entries.len() < GROUP_TARGET_POOL_MAX_COUNT
            && held_bytes + new_bytes <= GROUP_TARGET_POOL_MAX_BYTES
        {
            let (half_a_rtv, half_a_srv, half_b_rtv, half_b_srv) = half_fields(make_half());
            self.entries.push(GroupTargetEntry {
                rtv,
                srv,
                width,
                height,
                half_a_rtv,
                half_a_srv,
                half_b_rtv,
                half_b_srv,
                in_use: true,
                frame_used: true,
            });
            let entry = self.entries.last()?;
            return Some(GroupLease {
                rtv: entry.rtv.clone(),
                srv: entry.srv.clone(),
                half_a_rtv: entry.half_a_rtv.clone(),
                half_a_srv: entry.half_a_srv.clone(),
                half_b_rtv: entry.half_b_rtv.clone(),
                half_b_srv: entry.half_b_srv.clone(),
                origin,
                size: [width as f32, height as f32],
            });
        }
        // Replace a free entry (too small or too large for this group).
        if let Some(position) = self.entries.iter().position(|entry| !entry.in_use) {
            let held_without: u64 = self
                .entries
                .iter()
                .enumerate()
                .filter(|(index, _)| *index != position)
                .map(|(_, entry)| entry.bytes())
                .sum();
            if held_without + new_bytes <= GROUP_TARGET_POOL_MAX_BYTES {
                let (half_a_rtv, half_a_srv, half_b_rtv, half_b_srv) = half_fields(make_half());
                self.entries[position] = GroupTargetEntry {
                    rtv,
                    srv,
                    width,
                    height,
                    half_a_rtv,
                    half_a_srv,
                    half_b_rtv,
                    half_b_srv,
                    in_use: true,
                    frame_used: true,
                };
                let entry = &self.entries[position];
                return Some(GroupLease {
                    rtv: entry.rtv.clone(),
                    srv: entry.srv.clone(),
                    half_a_rtv: entry.half_a_rtv.clone(),
                    half_a_srv: entry.half_a_srv.clone(),
                    half_b_rtv: entry.half_b_rtv.clone(),
                    half_b_srv: entry.half_b_srv.clone(),
                    origin,
                    size: [width as f32, height as f32],
                });
            }
        }
        // Over the budget: the group renders inline, observably.
        self.overflow_count = self.overflow_count.saturating_add(1);
        #[cfg(debug_assertions)]
        {
            let (count, bytes, overflows) = self.stats();
            eprintln!(
                "gpui-go-native: group target pool over budget ({count} targets, {bytes} bytes;                  cap {GROUP_TARGET_POOL_MAX_COUNT} targets / {GROUP_TARGET_POOL_MAX_BYTES} bytes):                  the group renders inline (cumulative overflows: {overflows})"
            );
        }
        None
    }

    /// Releases the lease's backing at the group's end (the entry stays
    /// retained for the rest of the frame - the prune keeps what a group
    /// USED this frame, released or not).
    fn release(&mut self, lease: &GroupLease) {
        for entry in self.entries.iter_mut() {
            if entry.srv.is_some()
                && lease.rtv.is_some()
                && windows::core::Interface::as_raw(
                    entry.rtv.as_ref().expect("held entry has an rtv"),
                ) == windows::core::Interface::as_raw(lease.rtv.as_ref().expect("lease rtv"))
            {
                entry.in_use = false;
                return;
            }
        }
    }

    /// The frame-end prune (viz's UpdateRenderPassTextures rule): keep
    /// the backings a group used this frame (acquired-and-released
    /// included - the reuse across frames is the point), drop the
    /// stale ones, and re-arm the frame-used flags.
    fn prune_unused(&mut self) {
        self.entries.retain(|entry| entry.frame_used);
        for entry in self.entries.iter_mut() {
            entry.frame_used = false;
        }
    }
}

/// The render walk's active render target: the swapchain (or the
/// offscreen scene target), or an open group's pooled bucket. The
/// offset viewport maps window coordinates into the target (a bucket's
/// children draw at window position minus the bucket origin), and
/// `origin`/`size` carry the same mapping for the composites' uv math.
#[derive(Clone)]
struct ActiveTarget {
    rtv: Option<ID3D11RenderTargetView>,
    srv: Option<ID3D11ShaderResourceView>,
    viewport: D3D11_VIEWPORT,
    origin: [f32; 2],
    size: [f32; 2],
}

/// Group 0 occupies registers 0 and 1 in native shaders, so generated
/// group-1 bindings start at 2 (the pin's `GROUP_1_REGISTER_OFFSET`).
const GROUP_1_REGISTER_OFFSET: u32 = 2;
const DATA_REGISTER: u32 = shader_interface::DATA_BUFFER_BINDING + GROUP_1_REGISTER_OFFSET;
const PRIMARY_TEXTURE_REGISTER: u32 =
    shader_interface::PRIMARY_TEXTURE_BINDING + GROUP_1_REGISTER_OFFSET;
const PRIMARY_SAMPLER_REGISTER: u32 =
    shader_interface::PRIMARY_SAMPLER_BINDING + GROUP_1_REGISTER_OFFSET;
/// The surface sampler register of the SURFACES pipeline (bound per
/// imported capture surface — ticket28).
const SURFACE_SAMPLER_REGISTER: u32 =
    shader_interface::SURFACE_SAMPLER_BINDING + GROUP_1_REGISTER_OFFSET;
/// The gradient table's structured-buffer register (rev 16, ticket
/// 52: group(1) binding(5) -> t7; the quad/shadow fragments index it
/// by the background's gradient reference).
const GRADIENT_TABLE_REGISTER: u32 = 5 + GROUP_1_REGISTER_OFFSET;
/// The gradient ramp texture's register (rev 16, ticket 52: group(1)
/// binding(6) -> t8; the 1024-wide premultiplied-sRGB rows).
const GRADIENT_RAMP_REGISTER: u32 = 6 + GROUP_1_REGISTER_OFFSET;
/// The gradient ramp's clamp-addressing sampler register (rev 16,
/// ticket 52: group(1) binding(7) -> s9).
const GRADIENT_SAMPLER_REGISTER: u32 = 7 + GROUP_1_REGISTER_OFFSET;
/// The mask source texture's register (rev 16, ticket 52: the blur
/// module's group(1) binding(8) -> t10; the composite's mask sample -
/// the atlas texture for image masks, the coverage target for
/// clip-path masks).
const MASK_TEXTURE_REGISTER: u32 = 8 + GROUP_1_REGISTER_OFFSET;
/// The blend composite's backdrop-copy texture register (rev 16,
/// ticket 52: the blur module's group(1) binding(9) -> t11).
const BACKDROP_TEXTURE_REGISTER: u32 = 9 + GROUP_1_REGISTER_OFFSET;

/// Background appearance tags of the draw entries (the pin's
/// `WindowBackgroundAppearance`, reduced to the opaque/transparent
/// distinction its `render` method makes for the clear color).
pub mod background_appearance {
    /// Opaque: the pin clears with `[1.0; 4]`.
    pub const OPAQUE: u32 = 0;
    /// Transparent / alpha-blended: the pin clears with `[0.0; 4]`.
    pub const TRANSPARENT: u32 = 1;
}

// ---------------------------------------------------------------------------
// Shader modules (the pin's shader_resources::ShaderModule)
// ---------------------------------------------------------------------------

/// One pipeline's DXBC module, resolved from the build-time artifact
/// table (the pin's `directx_renderer::shader_resources::ShaderModule`).
#[derive(Copy, Clone, Debug, Eq, PartialEq)]
pub(super) enum ShaderModule {
    Quad,
    SmoothedQuad,
    Shadow,
    SmoothedShadow,
    Underline,
    PathRasterization,
    PathSprite,
    MonochromeSprite,
    SubpixelSprite,
    PolychromeSprite,
    SmoothedPolychromeSprite,
    Surface,
    BlurDownsample,
    Blur,
    BlurComposite,
    SmoothedBlurComposite,
    /// The transformed-group composite (rev 16, ticket 51): the quad
    /// under the effect group's 2x3 matrix over the local-space
    /// subtree bucket.
    TransformedBlurComposite,
    /// The blend composite (rev 16, ticket 52): the W3C blend formula
    /// against a backdrop copy of the parent target.
    BlendComposite,
    /// The drop-shadow composite (rev 16, ticket 52): the blurred
    /// silhouette tinted and drawn beneath the content quad.
    DropShadowComposite,
    /// The vendored backdrop mirror-crop pass (rev 15).
    BackdropCrop,
}

impl ShaderModule {
    fn label(self) -> &'static str {
        match self {
            Self::Quad => "quads",
            Self::SmoothedQuad => "smoothed_quads",
            Self::Shadow => "shadows",
            Self::SmoothedShadow => "smoothed_shadows",
            Self::Underline => "underlines",
            Self::PathRasterization => "path_rasterization",
            Self::PathSprite => "paths",
            Self::MonochromeSprite => "monochrome_sprites",
            Self::SubpixelSprite => "subpixel_sprites",
            Self::PolychromeSprite => "polychrome_sprites",
            Self::SmoothedPolychromeSprite => "smoothed_polychrome_sprites",
            Self::Surface => "surfaces",
            Self::BlurDownsample => "blur_downsample",
            Self::Blur => "blur",
            Self::BlurComposite => "blur_composite",
            Self::SmoothedBlurComposite => "smoothed_blur_composite",
            Self::TransformedBlurComposite => "transformed_blur_composite",
            Self::BlendComposite => "blend_composite",
            Self::DropShadowComposite => "drop_shadow_composite",
            Self::BackdropCrop => "backdrop_crop",
        }
    }

    fn native_shader(self) -> &'static gpui_render::artifacts::NativeShader {
        let label = self.label();
        NATIVE_SHADERS
            .iter()
            .find(|shader| shader.label == label)
            .unwrap_or_else(|| panic!("missing generated native shader {label}"))
    }

    /// Both compiled stages plus the draw-constants register the vertex
    /// stage expects (the pin's `bytecode()`; runtime HLSL compilation
    /// is intentionally unsupported). The six rev-15 pipelines (quads,
    /// shadows, the blur composites and the backdrop crop) resolve to
    /// the vendored, patched DXBC this crate's build script compiles
    /// from `native/shaders/` — the recorded divergences (drop-shadow
    /// border-box clip-out, CSS gradient geometry and premultiplied
    /// sRGB mixing, the colour-matrix composite and the backdrop crop)
    /// live there; every other module keeps the pinned artifact blobs.
    fn bytecode(self) -> Result<StageBytecode, i32> {
        match self {
            Self::Quad => Ok(vendored_bytecode(
                "quads",
                include_bytes!(concat!(env!("OUT_DIR"), "/quads.vs_5_0.dxbc")),
                include_bytes!(concat!(env!("OUT_DIR"), "/quads.ps_5_0.dxbc")),
                true,
            )),
            Self::SmoothedQuad => Ok(vendored_bytecode(
                "smoothed_quads",
                include_bytes!(concat!(env!("OUT_DIR"), "/smoothed_quads.vs_5_0.dxbc")),
                include_bytes!(concat!(env!("OUT_DIR"), "/smoothed_quads.ps_5_0.dxbc")),
                true,
            )),
            Self::Shadow => Ok(vendored_bytecode(
                "shadows",
                include_bytes!(concat!(env!("OUT_DIR"), "/shadows.vs_5_0.dxbc")),
                include_bytes!(concat!(env!("OUT_DIR"), "/shadows.ps_5_0.dxbc")),
                true,
            )),
            Self::SmoothedShadow => Ok(vendored_bytecode(
                "smoothed_shadows",
                include_bytes!(concat!(env!("OUT_DIR"), "/smoothed_shadows.vs_5_0.dxbc")),
                include_bytes!(concat!(env!("OUT_DIR"), "/smoothed_shadows.ps_5_0.dxbc")),
                true,
            )),
            Self::BlurComposite => Ok(vendored_bytecode(
                "blur_composite",
                include_bytes!(concat!(env!("OUT_DIR"), "/blur_composite.vs_5_0.dxbc")),
                include_bytes!(concat!(env!("OUT_DIR"), "/blur_composite.ps_5_0.dxbc")),
                false,
            )),
            Self::SmoothedBlurComposite => Ok(vendored_bytecode(
                "smoothed_blur_composite",
                include_bytes!(concat!(env!("OUT_DIR"), "/smoothed_blur_composite.vs_5_0.dxbc")),
                include_bytes!(concat!(env!("OUT_DIR"), "/smoothed_blur_composite.ps_5_0.dxbc")),
                false,
            )),
            Self::TransformedBlurComposite => Ok(vendored_bytecode(
                "transformed_blur_composite",
                include_bytes!(concat!(env!("OUT_DIR"), "/transformed_blur_composite.vs_5_0.dxbc")),
                include_bytes!(concat!(env!("OUT_DIR"), "/transformed_blur_composite.ps_5_0.dxbc")),
                false,
            )),
            Self::BlendComposite => Ok(vendored_bytecode(
                "blend_composite",
                include_bytes!(concat!(env!("OUT_DIR"), "/blend_composite.vs_5_0.dxbc")),
                include_bytes!(concat!(env!("OUT_DIR"), "/blend_composite.ps_5_0.dxbc")),
                false,
            )),
            Self::DropShadowComposite => Ok(vendored_bytecode(
                "drop_shadow_composite",
                include_bytes!(concat!(env!("OUT_DIR"), "/drop_shadow_composite.vs_5_0.dxbc")),
                include_bytes!(concat!(env!("OUT_DIR"), "/drop_shadow_composite.ps_5_0.dxbc")),
                false,
            )),
            Self::BackdropCrop => Ok(vendored_bytecode(
                "backdrop_crop",
                include_bytes!(concat!(env!("OUT_DIR"), "/backdrop_crop.vs_5_0.dxbc")),
                include_bytes!(concat!(env!("OUT_DIR"), "/backdrop_crop.ps_5_0.dxbc")),
                false,
            )),
            _ => match self.native_shader().dx11 {
                Dx11Shader::Sm50(bytecode) => Ok(StageBytecode {
                    vertex: bytecode.vertex,
                    fragment: bytecode.fragment,
                    draw_constants: bytecode.draw_constants.map(|binding| binding.register),
                }),
                Dx11Shader::NativeWindowsBuildRequired => Err(renderer_status::ERR_DEVICE),
            },
        }
    }
}

/// One pipeline's resolved DXBC stages plus the draw-constants register
/// the vertex stage expects (the vendored and pinned sources' common
/// shape; rev 15).
struct StageBytecode {
    vertex: &'static [u8],
    fragment: &'static [u8],
    draw_constants: Option<u32>,
}

/// Wraps the vendored DXBC blobs. The instanced pipelines (quads,
/// shadows) carry the draw-constants register the pinned table assigns
/// (`DX11_DRAW_CONSTANTS_REGISTER`, b3 — the vendored interface table
/// pins the same value); the blur-layout pipelines (the composites and
/// the backdrop crop) carry none.
fn vendored_bytecode(
    label: &str,
    vertex: &'static [u8],
    fragment: &'static [u8],
    draw_constants: bool,
) -> StageBytecode {
    let _ = label;
    StageBytecode {
        vertex,
        fragment,
        draw_constants: draw_constants.then_some(shader_interface::DX11_DRAW_CONSTANTS_REGISTER),
    }
}

// ---------------------------------------------------------------------------
// Font info (the pin's get_font_info)
// ---------------------------------------------------------------------------

/// The DirectWrite rasterization parameters the font uniforms carry
/// (the pin's `FontInfo`).
pub(super) struct FontInfo {
    pub gamma_ratios: [f32; 4],
    pub grayscale_enhanced_contrast: f32,
    pub subpixel_enhanced_contrast: f32,
    pub is_bgr: bool,
}

/// The pin's `DirectXRenderer::get_font_info` (queried once per
/// process through the shared DirectWrite factory).
fn font_info() -> &'static FontInfo {
    static CACHED_FONT_INFO: OnceLock<FontInfo> = OnceLock::new();
    CACHED_FONT_INFO.get_or_init(|| unsafe {
        let factory: IDWriteFactory5 = DWriteCreateFactory(DWRITE_FACTORY_TYPE_SHARED)
            .expect("creating the DirectWrite factory for the font uniforms");
        let render_params: IDWriteRenderingParams1 = factory
            .CreateRenderingParams()
            .expect("creating the DirectWrite rendering params")
            .cast()
            .expect("the shared rendering params expose IDWriteRenderingParams1");
        FontInfo {
            gamma_ratios: gpui::get_gamma_correction_ratios(render_params.GetGamma()),
            grayscale_enhanced_contrast: render_params.GetGrayscaleEnhancedContrast(),
            subpixel_enhanced_contrast: render_params.GetEnhancedContrast(),
            is_bgr: render_params.GetPixelGeometry() == DWRITE_PIXEL_GEOMETRY_BGR,
        }
    })
}

// ---------------------------------------------------------------------------
// Global elements (the pin's DirectXGlobalElements)
// ---------------------------------------------------------------------------

/// The global constant buffers (registers b0 globals, b1 font
/// rasterization, b3 per-draw draw constants) and the shared sampler
/// (the pin's `DirectXGlobalElements`). Cloning AddRefs the COM handles
/// so one frame's bindings can outlive the borrow of the pipeline set.
#[derive(Clone)]
struct GlobalElements {
    globals_buffer: ID3D11Buffer,
    font_buffer: ID3D11Buffer,
    draw_constants_buffer: ID3D11Buffer,
    sampler: Option<ID3D11SamplerState>,
    /// The nearest-neighbour sampler of pixelated sprite batches (CSS
    /// `image-rendering: pixelated`; rev 15).
    point_sampler: Option<ID3D11SamplerState>,
    /// The mirror-addressing sampler of the backdrop crop's blur passes
    /// (CSS filter-effects-2's mirrored backdrop edges; rev 15).
    mirror_sampler: Option<ID3D11SamplerState>,
    /// The clamp-addressing linear sampler of the gradient ramp rows
    /// (rev 16, ticket 52: the baked 1024x1 rows pad at their ends -
    /// the ramp's own first/last stop colors - so the pad tile's
    /// clamp IS the sampling rule).
    clamp_sampler: Option<ID3D11SamplerState>,
}

impl GlobalElements {
    /// The global constant buffers at registers b0/b1
    /// (`DirectXGlobalElements::cbuffers`).
    fn cbuffers(&self) -> [Option<ID3D11Buffer>; 2] {
        [Some(self.globals_buffer.clone()), Some(self.font_buffer.clone())]
    }

    fn new(device: &ID3D11Device) -> Result<Self, i32> {
        let globals_buffer =
            create_constant_buffer(device, std::mem::size_of::<GlobalUniforms>())?;
        let font_buffer =
            create_constant_buffer(device, std::mem::size_of::<FontRasterizationUniforms>())?;
        let draw_constants_buffer =
            create_constant_buffer(device, std::mem::size_of::<Dx11DrawConstants>())?;
        let make_sampler = |filter: D3D11_FILTER, address: D3D11_TEXTURE_ADDRESS_MODE| {
            unsafe {
                let desc = D3D11_SAMPLER_DESC {
                    Filter: filter,
                    AddressU: address,
                    AddressV: address,
                    AddressW: address,
                    MipLODBias: 0.0,
                    MaxAnisotropy: 1,
                    ComparisonFunc: D3D11_COMPARISON_ALWAYS,
                    BorderColor: [0.0; 4],
                    MinLOD: 0.0,
                    MaxLOD: D3D11_FLOAT32_MAX,
                };
                let mut output = None;
                device.CreateSamplerState(&desc, Some(&mut output)).map_err(|e| e.code().0)?;
                output.ok_or(renderer_status::ERR_DEVICE)
            }
        };
        let sampler = make_sampler(D3D11_FILTER_MIN_MAG_MIP_LINEAR, D3D11_TEXTURE_ADDRESS_WRAP)?;
        let point_sampler = make_sampler(D3D11_FILTER_MIN_MAG_MIP_POINT, D3D11_TEXTURE_ADDRESS_WRAP)?;
        let mirror_sampler =
            make_sampler(D3D11_FILTER_MIN_MAG_MIP_LINEAR, D3D11_TEXTURE_ADDRESS_MIRROR)?;
        let clamp_sampler =
            make_sampler(D3D11_FILTER_MIN_MAG_MIP_LINEAR, D3D11_TEXTURE_ADDRESS_CLAMP)?;
        Ok(Self {
            globals_buffer,
            font_buffer,
            draw_constants_buffer,
            sampler: Some(sampler),
            point_sampler: Some(point_sampler),
            mirror_sampler: Some(mirror_sampler),
            clamp_sampler: Some(clamp_sampler),
        })
    }
}

// ---------------------------------------------------------------------------
// The rev-15 composite and backdrop-crop extensions (browser-core
// ticket 39)
// ---------------------------------------------------------------------------

/// The composite pass's extension payload, mirroring the vendored
/// The composite extension cbuffer's matrix capacity (rev 15.1): one
/// entry per colour function, mirroring the vendored shader's
/// `ColorMatrixUniforms.entries` array.
const COLOR_MATRIX_CAPACITY: u32 = 8;

/// shader's `ColorMatrixUniforms` (the filter chain's per-function
/// colour matrices and the composite's region-relative sampling
/// offset). 656 bytes — 8 entries of 80 (each a row-major 4x5 matrix:
/// four `row_i` vectors plus the `offset` fifth column), the sampling
/// offset, the entry count and padding. Rev 15.1 (browser-core ticket
/// 77) replaced rev 15's single 112-byte composed matrix with the
/// per-function entries: Chrome clamps each filter function's output
/// to [0,1] before the next applies (each function's result is
/// rasterized 8-bit), which one composed linear map cannot express.
#[repr(C)]
#[derive(Clone, Copy)]
struct ColorMatrixEntry {
    row0: [f32; 4],
    row1: [f32; 4],
    row2: [f32; 4],
    row3: [f32; 4],
    offset: [f32; 4],
}

#[repr(C)]
#[derive(Clone, Copy)]
struct CompositeExtensionsUniforms {
    entries: [ColorMatrixEntry; 8],
    sample_offset: [f32; 2],
    count: u32,
    padding0: u32,
    /// The composite target's window-space origin (rev 16, ticket 51):
    /// SV_Position is target-frame - a nested group composites into
    /// an offset pooled bucket - so the vendored composite shaders add
    /// this to recover the window position. (0, 0) for the window and
    /// the offscene target.
    target_origin: [f32; 2],
    padding2: [f32; 2],
    /// The transformed-group composite's 2x3 matrix rows (rev 16,
    /// ticket 51): `transform_row0` = (m00, m01, tx, 0) and
    /// `transform_row1` = (m10, m11, ty, 0) - the ABI record's
    /// row-major layout (device = row.xy . p + row.z). The unit
    /// matrix for every other composite pass.
    transform_row0: [f32; 4],
    transform_row1: [f32; 4],
    /// The composite's MASK source kind (rev 16, ticket 52): 0 none,
    /// 1 gradient table, 2 atlas texture, 3 coverage, 4
    /// invalid/unresolvable (the composite draws NOTHING - the
    /// design §3.5's "an invalid mask hides the element").
    mask_kind: u32,
    /// The mask source reference (the gradient table index / the
    /// atlas texture index / the coverage entry index).
    mask_reference: u32,
    /// 0 alpha, 1 luminance (the mask mode).
    mask_mode: u32,
    /// The composite's blend mode (rev 16, ticket 52): 0 src-over ..
    /// 16 plus-lighter - the W3C compositing tags; only the BLEND
    /// composite pass reads it (every other composite leaves 0).
    blend_mode: u32,
    /// The drop-shadow composite's tint color (rev 16, ticket 52;
    /// straight sRGBA, used only by the shadow pass).
    tint_color: [f32; 4],
    /// The blend composite's backdrop-copy size (rev 16, ticket 52;
    /// the backdrop is sampled at device position / this size).
    backdrop_size: [f32; 2],
    /// 1 for the drop-shadow composite pass (sample the silhouette,
    /// tint and draw; rev 16, ticket 52), 0 otherwise.
    shadow_mode: u32,
    padding3: u32,
}

impl Default for CompositeExtensionsUniforms {
    fn default() -> Self {
        Self {
            entries: [ColorMatrixEntry {
                row0: [1.0, 0.0, 0.0, 0.0],
                row1: [0.0, 1.0, 0.0, 0.0],
                row2: [0.0, 0.0, 1.0, 0.0],
                row3: [0.0, 0.0, 0.0, 1.0],
                offset: [0.0; 4],
            }; 8],
            sample_offset: [0.0; 2],
            count: 0,
            padding0: 0,
            target_origin: [0.0, 0.0],
            padding2: [0.0, 0.0],
            transform_row0: [1.0, 0.0, 0.0, 0.0],
            transform_row1: [0.0, 1.0, 0.0, 0.0],
            mask_kind: 0,
            mask_reference: 0,
            mask_mode: 0,
            blend_mode: 0,
            tint_color: [0.0; 4],
            backdrop_size: [1.0, 1.0],
            shadow_mode: 0,
            padding3: 0,
        }
    }
}

/// The backdrop mirror-crop pass's uniform, mirroring the vendored
/// shader's `BackdropCropUniforms`. 32 bytes.
#[repr(C)]
#[derive(Clone, Copy, Default)]
struct BackdropCropUniforms {
    origin: [f32; 2],
    source_size: [f32; 2],
    region_size: [f32; 2],
    margin: [f32; 2],
}

/// Which extra bindings one blur pass carries (rev 15): the composite's
/// colour-matrix extension cbuffer (b5), the crop pass's uniform (b6),
/// or none (the pinned passes).
#[derive(Clone, Copy)]
enum BlurPassExtras<'a> {
    None,
    Composite(&'a CompositeExtensionsUniforms),
    Crop(&'a BackdropCropUniforms),
}

/// One backdrop's crop state: the source region, the mirrored margins,
/// the crop layout size (`region + 2*margin`, exactly the texture's
/// size — the passes' normalized coordinates require the match), and
/// the crop texture's views.
struct BackdropCropRegion {
    origin: [f32; 2],
    region: [f32; 2],
    margin: [f32; 2],
    total: [f32; 2],
    rtv: Option<ID3D11RenderTargetView>,
    srv: Option<ID3D11ShaderResourceView>,
    half_a_rtv: Option<ID3D11RenderTargetView>,
    half_a_srv: Option<ID3D11ShaderResourceView>,
    half_b_rtv: Option<ID3D11RenderTargetView>,
    half_b_srv: Option<ID3D11ShaderResourceView>,
}

// ---------------------------------------------------------------------------
// The instanced pipeline (the pin's PipelineState<T>, byte-oriented)
// ---------------------------------------------------------------------------

struct PipelineVariant {
    specification: &'static shader_interface::Pipeline,
    vertex: ID3D11VertexShader,
    fragment: ID3D11PixelShader,
}

/// One generated instanced pipeline plus its whole-frame instance
/// buffer (the pin's `PipelineState<T>`; the buffer is byte-oriented
/// here because the ABI scene snapshot owns the records, but the
/// capacity bookkeeping and every binding are the pin's).
struct InstancePipeline {
    #[expect(dead_code)]
    label: &'static str,
    specification: &'static shader_interface::Pipeline,
    vertex: ID3D11VertexShader,
    fragment: ID3D11PixelShader,
    variant: Option<PipelineVariant>,
    draw_constants_register: u32,
    buffer: ID3D11Buffer,
    /// Capacity in elements (the pin's `buffer_size`).
    buffer_capacity: usize,
    element_size: usize,
    view: Option<ID3D11ShaderResourceView>,
    blend_state: ID3D11BlendState,
}

impl InstancePipeline {
    fn new(
        device: &ID3D11Device,
        label: &'static str,
        module: ShaderModule,
        element_size: usize,
        initial_capacity: usize,
        blend_state: ID3D11BlendState,
    ) -> Result<Self, i32> {
        let shader = module.native_shader();
        let bytecode = module.bytecode()?;
        let draw_constants = bytecode
            .draw_constants
            .ok_or(renderer_status::ERR_DEVICE)?;
        let vertex = create_vertex_shader(device, bytecode.vertex)?;
        let fragment = create_fragment_shader(device, bytecode.fragment)?;
        let (buffer, view) = create_instance_buffer(device, element_size, initial_capacity)?;
        Ok(Self {
            label,
            specification: shader.pipeline,
            vertex,
            fragment,
            variant: None,
            draw_constants_register: draw_constants,
            buffer,
            buffer_capacity: initial_capacity,
            element_size,
            view,
            blend_state,
        })
    }

    /// The pin's `with_variant`: the smoothed pipeline of the same
    /// layout (checked) with its own shader objects.
    fn with_variant(mut self, device: &ID3D11Device, module: ShaderModule) -> Result<Self, i32> {
        let shader = module.native_shader();
        let bytecode = module.bytecode()?;
        if shader.pipeline.data_layout != self.specification.data_layout
            || shader.pipeline.topology != self.specification.topology
            || shader.pipeline.vertex_count != self.specification.vertex_count
        {
            return Err(renderer_status::ERR_DEVICE);
        }
        let draw_constants = bytecode
            .draw_constants
            .ok_or(renderer_status::ERR_DEVICE)?;
        if draw_constants != self.draw_constants_register {
            return Err(renderer_status::ERR_DEVICE);
        }
        self.variant = Some(PipelineVariant {
            specification: shader.pipeline,
            vertex: create_vertex_shader(device, bytecode.vertex)?,
            fragment: create_fragment_shader(device, bytecode.fragment)?,
        });
        Ok(self)
    }

    /// The pin's `update_buffer`: grow to the next power of two (the
    /// 256MB ceiling), then `MAP_WRITE_DISCARD` the whole frame's
    /// instances in one upload.
    fn update(&mut self, device: &ID3D11Device, _ctx: &ID3D11DeviceContext, element_count: usize) -> Result<(), i32> {
        if self.buffer_capacity < element_count {
            if self.element_size == 0 {
                return Err(renderer_status::ERR_DEVICE);
            }
            let required_size = self
                .element_size
                .checked_mul(element_count)
                .ok_or(renderer_status::ERR_BAD_VALUE)?;
            if required_size > MAX_INSTANCE_BUFFER_SIZE {
                return Err(renderer_status::ERR_CAPACITY);
            }
            let max_elements = MAX_INSTANCE_BUFFER_SIZE / self.element_size;
            let new_capacity = element_count
                .checked_next_power_of_two()
                .unwrap_or(max_elements)
                .min(max_elements);
            if new_capacity < element_count {
                return Err(renderer_status::ERR_CAPACITY);
            }
            let (buffer, view) = create_instance_buffer(device, self.element_size, new_capacity)?;
            self.buffer = buffer;
            self.view = view;
            self.buffer_capacity = new_capacity;
        }
        Ok(())
    }

    /// The pin's `draw_instances` (a fixed vertex count per instance).
    fn draw_instances(
        &self,
        frame: &FrameBindings<'_>,
        texture: Option<&[Option<ID3D11ShaderResourceView>; 1]>,
        first: u32,
        count: u32,
    ) -> Result<(), i32> {
        self.draw_instances_sampling(frame, texture, first, count, false)
    }

    /// `draw_instances` with the sampling choice (rev 15): a pixelated
    /// batch binds the point sampler instead of the shared bilinear one
    /// (CSS `image-rendering: pixelated`).
    fn draw_instances_sampling(
        &self,
        frame: &FrameBindings<'_>,
        texture: Option<&[Option<ID3D11ShaderResourceView>; 1]>,
        first: u32,
        count: u32,
        pixelated: bool,
    ) -> Result<(), i32> {
        let vertex_count = self
            .specification
            .vertex_count
            .fixed()
            .ok_or(renderer_status::ERR_DEVICE)?;
        self.draw_with_variant(frame, texture, vertex_count, first, count, false, pixelated)
    }

    /// The pin's `draw_instances_variant` (the smoothed variant toggle),
    /// with the sampling choice (rev 15).
    fn draw_instances_variant(
        &self,
        frame: &FrameBindings<'_>,
        texture: Option<&[Option<ID3D11ShaderResourceView>; 1]>,
        first: u32,
        count: u32,
        use_variant: bool,
        pixelated: bool,
    ) -> Result<(), i32> {
        let specification = if use_variant {
            self.variant
                .as_ref()
                .ok_or(renderer_status::ERR_DEVICE)?
                .specification
        } else {
            self.specification
        };
        let vertex_count = specification
            .vertex_count
            .fixed()
            .ok_or(renderer_status::ERR_DEVICE)?;
        self.draw_with_variant(frame, texture, vertex_count, first, count, use_variant, pixelated)
    }

    /// The pin's `draw_vertices` (vertex-pulled, one instance).
    fn draw_vertices(&self, frame: &FrameBindings<'_>, vertex_count: u32) -> Result<(), i32> {
        if self.specification.vertex_count.fixed().is_some() {
            return Err(renderer_status::ERR_DEVICE);
        }
        self.draw_with_variant(frame, None, vertex_count, 0, 1, false, false)
    }

    /// The pin's `draw_with_variant`: the single place that issues an
    /// instanced draw. The batch base reaches the shader through the
    /// draw-constants cbuffer (`StartInstanceLocation` stays zero, the
    /// shader adds the base itself).
    #[allow(clippy::too_many_arguments)]
    fn draw_with_variant(
        &self,
        frame: &FrameBindings<'_>,
        texture: Option<&[Option<ID3D11ShaderResourceView>; 1]>,
        vertex_count: u32,
        first: u32,
        count: u32,
        use_variant: bool,
        pixelated: bool,
    ) -> Result<(), i32> {
        if count == 0 || vertex_count == 0 {
            return Ok(());
        }
        if first as usize + count as usize > self.buffer_capacity {
            return Err(renderer_status::ERR_CAPACITY);
        }
        let ctx = frame.device_context;
        update_buffer(
            ctx,
            &frame.globals.draw_constants_buffer,
            &[Dx11DrawConstants::for_instances(first)],
        )?;
        let specification = if use_variant {
            self.variant
                .as_ref()
                .ok_or(renderer_status::ERR_DEVICE)?
                .specification
        } else {
            self.specification
        };
        let topology = match specification.topology {
            shader_interface::PrimitiveTopology::TriangleList => {
                D3D_PRIMITIVE_TOPOLOGY_TRIANGLELIST
            }
            shader_interface::PrimitiveTopology::TriangleStrip => {
                D3D_PRIMITIVE_TOPOLOGY_TRIANGLESTRIP
            }
        };
        let binds = frame.binds.clone();
        unsafe {
            binds.bind_data_srv(ctx, self.view.as_ref());
            binds.bind_topology(ctx, topology);
            binds.bind_viewport(ctx, &frame.viewport);
            if use_variant {
                let variant = self.variant.as_ref().ok_or(renderer_status::ERR_DEVICE)?;
                binds.bind_vertex_shader(ctx, &variant.vertex);
                binds.bind_pixel_shader(ctx, &variant.fragment);
            } else {
                binds.bind_vertex_shader(ctx, &self.vertex);
                binds.bind_pixel_shader(ctx, &self.fragment);
            }
            binds.bind_cbuffer_pair(ctx, &frame.globals);
            binds.bind_cbuffer_data(
                ctx,
                self.draw_constants_register,
                &frame.globals.draw_constants_buffer,
                BindStages::Vertex,
            );
            binds.bind_blend_state(ctx, &self.blend_state);
            if let Some(texture) = texture {
                // rev 15: a pixelated batch samples nearest-neighbour.
                let sampler = if pixelated {
                    frame.globals.point_sampler.clone()
                } else {
                    frame.globals.sampler.clone()
                };
                if let Some(sampler) = sampler.as_ref() {
                    binds.bind_primary_sampler(ctx, sampler);
                }
                // The vertex stage reads the atlas dimensions for tile
                // coordinates.
                let texture_ref = texture[0].as_ref();
                binds.bind_primary_srv_vertex(ctx, texture_ref);
                binds.bind_primary_srv_pixel(ctx, texture_ref);
            }
            // `StartInstanceLocation` stays zero: the shader adds the
            // base itself.
            ctx.DrawInstanced(vertex_count, count, 0, 0);
        }
        Ok(())
    }
}

/// The pin's `SurfacePipeline`: one draw per surface with per-draw
/// uniforms. Constructed exactly like the pin's; the ABI surface source
/// The pin's `SurfacePipeline`: one draw per surface with per-draw
/// uniforms; bound per imported capture surface (ticket28).
struct SurfacePipeline {
    vertex: ID3D11VertexShader,
    fragment: ID3D11PixelShader,
    params_buffer: ID3D11Buffer,
    blend: ID3D11BlendState,
}

/// The whole draw-pipeline set (the pin's `DirectXRenderPipelines`).
pub(super) struct DrawPipelines {
    shadow_pipeline: InstancePipeline,
    quad_pipeline: InstancePipeline,
    path_rasterization_pipeline: InstancePipeline,
    path_sprite_pipeline: InstancePipeline,
    underline_pipeline: InstancePipeline,
    mono_sprites: InstancePipeline,
    subpixel_sprites: InstancePipeline,
    poly_sprites: InstancePipeline,
    /// Constructed like the pin's; the surface draw path binds it per
    /// imported capture surface (ticket28).
    surfaces: SurfacePipeline,
    // Blur: not the generic pipeline, since these sample a texture
    // instead of reading a structured instance buffer; parameters live
    // in a cbuffer at DATA_REGISTER.
    blur_downsample_vertex: ID3D11VertexShader,
    blur_downsample_fragment: ID3D11PixelShader,
    blur_vertex: ID3D11VertexShader,
    blur_fragment: ID3D11PixelShader,
    blur_composite_vertex: ID3D11VertexShader,
    blur_composite_fragment: ID3D11PixelShader,
    smoothed_blur_composite_vertex: ID3D11VertexShader,
    smoothed_blur_composite_fragment: ID3D11PixelShader,
    /// The transformed-group composite (rev 16, ticket 51).
    transformed_blur_composite_vertex: ID3D11VertexShader,
    transformed_blur_composite_fragment: ID3D11PixelShader,
    /// The blend composite (rev 16, ticket 52): the W3C blend formula
    /// against the backdrop copy.
    blend_composite_vertex: ID3D11VertexShader,
    blend_composite_fragment: ID3D11PixelShader,
    /// The drop-shadow composite (rev 16, ticket 52): the blurred
    /// silhouette tinted beneath the content.
    drop_shadow_composite_vertex: ID3D11VertexShader,
    drop_shadow_composite_fragment: ID3D11PixelShader,
    /// The vendored backdrop mirror-crop pass (rev 15).
    backdrop_crop_vertex: ID3D11VertexShader,
    backdrop_crop_fragment: ID3D11PixelShader,
    blur_params_buffer: ID3D11Buffer,
    /// The composite extensions cbuffer (b5, the colour matrix and the
    /// sampling offset; rev 15).
    composite_extensions_buffer: ID3D11Buffer,
    /// The backdrop crop cbuffer (b6; rev 15).
    backdrop_crop_buffer: ID3D11Buffer,
    blur_blend_replace: ID3D11BlendState,
    blur_blend_composite: ID3D11BlendState,
    /// The gradient table's structured instance buffer (rev 16,
    /// ticket 52): one 64-byte `GradientInstance` per scene gradient
    /// (the ABI record reshaped for the HLSL structured-buffer layout;
    /// bound at t7 for the quad/shadow draws and the gradient masks).
    gradient_table_buffer: ID3D11Buffer,
    /// The gradient table's raw SRV (the shaders read it through a
    /// `RuntimeArray<GradientInstance>` view).
    gradient_table_view: Option<ID3D11ShaderResourceView>,
    /// The gradient table's capacity in elements (grown in place, the
    /// instance buffers' power-of-two rule).
    gradient_table_capacity: usize,
    /// The baked gradient ramp texture (rev 16, ticket 52): 1024-wide
    /// premultiplied-sRGB rows, one per gradient (the height grows with
    /// the scene's gradient count; a 1-row placeholder when empty).
    ramp_texture: Option<ID3D11Texture2D>,
    /// The ramp texture's SRV (bound at t8).
    ramp_srv: Option<ID3D11ShaderResourceView>,
    /// The ramp texture's current row count.
    ramp_rows: u32,
}

impl DrawPipelines {
    fn new(device: &ID3D11Device) -> Result<Self, i32> {
        // rev 16 (ticket 50): the quad and shadow pipelines' instance
        // stride is the ABI RECORD's size - the vendored shader structs
        // mirror `GpuiGoSceneQuadRecord`/`GpuiGoSceneShadowRecord` field
        // for field, and the draw path uploads the records directly.
        let shadow_pipeline = InstancePipeline::new(
            device,
            "shadow_pipeline",
            ShaderModule::Shadow,
            std::mem::size_of::<crate::scene::GpuiGoSceneShadowRecord>(),
            4,
            create_blend_state(device)?,
        )?
        .with_variant(device, ShaderModule::SmoothedShadow)?;
        let quad_pipeline = InstancePipeline::new(
            device,
            "quad_pipeline",
            ShaderModule::Quad,
            std::mem::size_of::<crate::scene::GpuiGoSceneQuadRecord>(),
            64,
            create_blend_state(device)?,
        )?
        .with_variant(device, ShaderModule::SmoothedQuad)?;
        let path_rasterization_pipeline = InstancePipeline::new(
            device,
            "path_rasterization_pipeline",
            ShaderModule::PathRasterization,
            std::mem::size_of::<PathRasterizationVertex>(),
            32,
            create_blend_state_for_path_rasterization(device)?,
        )?;
        let path_sprite_pipeline = InstancePipeline::new(
            device,
            "path_sprite_pipeline",
            ShaderModule::PathSprite,
            std::mem::size_of::<PathSprite>(),
            4,
            create_blend_state_for_path_sprite(device)?,
        )?;
        let underline_pipeline = InstancePipeline::new(
            device,
            "underline_pipeline",
            ShaderModule::Underline,
            std::mem::size_of::<gpui::Underline>(),
            4,
            create_blend_state(device)?,
        )?;
        let mono_sprites = InstancePipeline::new(
            device,
            "monochrome_sprite_pipeline",
            ShaderModule::MonochromeSprite,
            std::mem::size_of::<gpui::MonochromeSprite>(),
            512,
            create_blend_state(device)?,
        )?;
        let subpixel_sprites = InstancePipeline::new(
            device,
            "subpixel_sprite_pipeline",
            ShaderModule::SubpixelSprite,
            std::mem::size_of::<gpui::SubpixelSprite>(),
            512,
            create_blend_state_for_subpixel_rendering(device)?,
        )?;
        let poly_sprites = InstancePipeline::new(
            device,
            "polychrome_sprite_pipeline",
            ShaderModule::PolychromeSprite,
            std::mem::size_of::<gpui::PolychromeSprite>(),
            16,
            create_blend_state(device)?,
        )?
        .with_variant(device, ShaderModule::SmoothedPolychromeSprite)?;

        let blur_downsample = ShaderModule::BlurDownsample.bytecode()?;
        let blur_downsample_vertex = create_vertex_shader(device, blur_downsample.vertex)?;
        let blur_downsample_fragment = create_fragment_shader(device, blur_downsample.fragment)?;
        let blur = ShaderModule::Blur.bytecode()?;
        let blur_vertex = create_vertex_shader(device, blur.vertex)?;
        let blur_fragment = create_fragment_shader(device, blur.fragment)?;
        let blur_composite = ShaderModule::BlurComposite.bytecode()?;
        let blur_composite_vertex = create_vertex_shader(device, blur_composite.vertex)?;
        let blur_composite_fragment = create_fragment_shader(device, blur_composite.fragment)?;
        let smoothed_blur_composite = ShaderModule::SmoothedBlurComposite.bytecode()?;
        let smoothed_blur_composite_vertex =
            create_vertex_shader(device, smoothed_blur_composite.vertex)?;
        let smoothed_blur_composite_fragment =
            create_fragment_shader(device, smoothed_blur_composite.fragment)?;
        let transformed_blur_composite = ShaderModule::TransformedBlurComposite.bytecode()?;
        let transformed_blur_composite_vertex =
            create_vertex_shader(device, transformed_blur_composite.vertex)?;
        let transformed_blur_composite_fragment =
            create_fragment_shader(device, transformed_blur_composite.fragment)?;
        let blend_composite = ShaderModule::BlendComposite.bytecode()?;
        let blend_composite_vertex = create_vertex_shader(device, blend_composite.vertex)?;
        let blend_composite_fragment = create_fragment_shader(device, blend_composite.fragment)?;
        let drop_shadow_composite = ShaderModule::DropShadowComposite.bytecode()?;
        // The drop-shadow composite shares the plain composite's vertex
        // stage (the rect + clip-distances shape).
        let drop_shadow_composite_vertex = create_vertex_shader(device, drop_shadow_composite.vertex)?;
        let drop_shadow_composite_fragment =
            create_fragment_shader(device, drop_shadow_composite.fragment)?;
        let backdrop_crop = ShaderModule::BackdropCrop.bytecode()?;
        let backdrop_crop_vertex = create_vertex_shader(device, backdrop_crop.vertex)?;
        let backdrop_crop_fragment = create_fragment_shader(device, backdrop_crop.fragment)?;
        let blur_params_buffer =
            create_constant_buffer(device, std::mem::size_of::<BlurUniforms>())?;
        let composite_extensions_buffer = create_constant_buffer(
            device,
            std::mem::size_of::<CompositeExtensionsUniforms>(),
        )?;
        let backdrop_crop_buffer =
            create_constant_buffer(device, std::mem::size_of::<BackdropCropUniforms>())?;
        let blur_blend_replace = create_blend_state_no_blend(device)?;
        // Premultiplied (One / InvSrcAlpha) — the composite outputs a
        // premultiplied blurred sample; straight-alpha blending would
        // darken the faded edges.
        let blur_blend_composite = create_blend_state_for_path_sprite(device)?;

        // rev 16 (ticket 52): the gradient table's structured buffer and
        // the 1-row placeholder ramp (both grown in place by the upload
        // path).
        let (gradient_table_buffer, gradient_table_view) =
            create_instance_buffer(device, GRADIENT_INSTANCE_STRIDE, 16)?;
        let (ramp_texture, ramp_srv) = create_gradient_ramp_texture(device, 1)?;

        let surface = ShaderModule::Surface.bytecode()?;
        let surfaces = SurfacePipeline {
            vertex: create_vertex_shader(device, surface.vertex)?,
            fragment: create_fragment_shader(device, surface.fragment)?,
            params_buffer: create_constant_buffer(device, std::mem::size_of::<SurfaceUniforms>())?,
            blend: create_blend_state(device)?,
        };

        Ok(Self {
            shadow_pipeline,
            quad_pipeline,
            path_rasterization_pipeline,
            path_sprite_pipeline,
            underline_pipeline,
            mono_sprites,
            subpixel_sprites,
            poly_sprites,
            surfaces,
            blur_downsample_vertex,
            blur_downsample_fragment,
            blur_vertex,
            blur_fragment,
            blur_composite_vertex,
            blur_composite_fragment,
            smoothed_blur_composite_vertex,
            smoothed_blur_composite_fragment,
            transformed_blur_composite_vertex,
            transformed_blur_composite_fragment,
            blend_composite_vertex,
            blend_composite_fragment,
            drop_shadow_composite_vertex,
            drop_shadow_composite_fragment,
            backdrop_crop_vertex,
            backdrop_crop_fragment,
            blur_params_buffer,
            composite_extensions_buffer,
            backdrop_crop_buffer,
            blur_blend_replace,
            blur_blend_composite,
            gradient_table_buffer,
            gradient_table_view,
            gradient_table_capacity: 16,
            ramp_texture: Some(ramp_texture),
            ramp_srv,
            ramp_rows: 1,
        })
    }
}

/// The device-global draw state: the pipeline set and the global
/// elements (lazily created at the first scene draw; the shader objects
/// only depend on the device, and every draw is serialized behind the
/// renderer-global mutex).
pub(super) struct DrawGlobal {
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
    /// The owned texture clone — the pin's lifetime holder: the cache
    /// keeps the capture frame's destination texture alive while the
    /// view is cached (the field is held, not read).
    #[expect(dead_code)]
    pub(super) texture: ID3D11Texture2D,
    pub(super) srv: ID3D11ShaderResourceView,
}

// ---------------------------------------------------------------------------
// Per-surface draw targets (the pin's PathResources / BlurResources)
// ---------------------------------------------------------------------------

/// The pin's `PathResources`: the MSAA rasterization target, the
/// resolved intermediate and its SRV.
pub(super) struct PathResources {
    texture: ID3D11Texture2D,
    srv: Option<ID3D11ShaderResourceView>,
    msaa_texture: ID3D11Texture2D,
    msaa_view: Option<ID3D11RenderTargetView>,
}

impl PathResources {
    fn new(device: &ID3D11Device, width: u32, height: u32) -> Result<Self, i32> {
        let (texture, srv) = create_path_intermediate_texture(device, width, height)?;
        let (msaa_texture, msaa_view) =
            create_path_intermediate_msaa_texture_and_view(device, width, height)?;
        Ok(Self {
            texture,
            srv,
            msaa_texture,
            msaa_view,
        })
    }
}

/// The pin's `BlurResources`, reshaped in rev 16 (ticket 50): the
/// offscreen scene color and the half-resolution ping/pong scratch
/// remain; the per-depth full-window group targets are GONE (the
/// bounds-sized, bucketed `GroupTargetPool` replaces them - every
/// matched group of either kind gets an isolated pool target, with no
/// depth cap).
pub(super) struct BlurResources {
    scene_color_rtv: Option<ID3D11RenderTargetView>,
    scene_color_srv: Option<ID3D11ShaderResourceView>,
    ping_rtv: Option<ID3D11RenderTargetView>,
    ping_srv: Option<ID3D11ShaderResourceView>,
    pong_rtv: Option<ID3D11RenderTargetView>,
    pong_srv: Option<ID3D11ShaderResourceView>,
}

impl BlurResources {
    fn new(device: &ID3D11Device, width: u32, height: u32) -> Result<Self, i32> {
        let half_w = downsampled_dimension(width);
        let half_h = downsampled_dimension(height);
        let (_, scene_color_rtv, scene_color_srv) = create_color_target(device, width, height)?;
        let (_, ping_rtv, ping_srv) = create_color_target(device, half_w, half_h)?;
        let (_, pong_rtv, pong_srv) = create_color_target(device, half_w, half_h)?;
        Ok(Self {
            scene_color_rtv,
            scene_color_srv,
            ping_rtv,
            ping_srv,
            pong_rtv,
            pong_srv,
        })
    }
}

/// The per-surface backdrop crop texture (rev 15): one full-resolution
/// color target holding the cropped backdrop region plus its mirrored
/// margins, recreated whenever a backdrop needs a larger one (backdrops
/// draw strictly sequentially, so one scratch texture serves them all).
struct BackdropCropTarget {
    rtv: Option<Option<ID3D11RenderTargetView>>,
    srv: Option<Option<ID3D11ShaderResourceView>>,
    width: u32,
    height: u32,
    /// The crop's half-resolution blur scratch PAIR (created with the
    /// crop; the same size rule as the rev-15 window ping/pong, but
    /// relative to the CROP so the downsample/gaussian/composite chain
    /// is consistent - two textures, the chain ping-pongs between
    /// them).
    half_a_rtv: Option<ID3D11RenderTargetView>,
    half_a_srv: Option<ID3D11ShaderResourceView>,
    half_b_rtv: Option<ID3D11RenderTargetView>,
    half_b_srv: Option<ID3D11ShaderResourceView>,
}

/// The per-surface, size-dependent draw targets (dropped on resize like
/// the pin's `recreate_resources` - the pool with them, exactly the
/// design's "resize drops and rebuilds the pool" rule).
#[derive(Default)]
pub(super) struct DrawTargets {
    pub(super) path: Option<PathResources>,
    pub(super) blur: Option<BlurResources>,
    pub(super) backdrop_crop: Option<BackdropCropTarget>,
    /// The clip-path coverage rasterization resources (rev 16, ticket
    /// 52), created lazily when the scene carries coverage entries.
    pub(super) coverage: Option<CoverageResources>,
    /// The blend composite's backdrop copy target (rev 16, ticket 52),
    /// created lazily when the scene carries blend groups.
    pub(super) blend_backdrop: Option<BlendBackdropTarget>,
    /// The R16 group target pool (rev 16, ticket 50): bounds-sized,
    /// bucketed, kept across frames while groups keep using it.
    pub(super) group_pool: GroupTargetPool,
}

// ---------------------------------------------------------------------------
// The redundant state-bind tracker (rev 16, browser-core ticket 53,
// the design's §3.12)
// ---------------------------------------------------------------------------

/// The cumulative state-bind counter: every topology, viewport,
/// shader, cbuffer, blend-state, sampler and texture binding the
/// tracker actually issued. The renderer's debug-stats entry reports
/// it (device-global, like the tracker itself — the shared immediate
/// context keeps one pipeline state across surfaces and frames, and
/// the tracker is the only writer).
static STATE_BIND_COUNT: AtomicUsize = AtomicUsize::new(0);

/// `draw_with_variant` and `dx_blur_pass` rebind their full pipeline
/// state on every draw (`draw.rs`'s batch loop). The tracker caches the
/// last-bound value of every state slot those two paths bind and skips
/// the bind when it cannot have changed — pure renderer-internal, zero
/// pixel impact (the skipped bind is a no-op by definition), observable
/// only through [`STATE_BIND_COUNT`].
///
/// The tracker is per-FRAME (created in `render_scene`): between frames
/// the present path, readbacks and other surfaces touch the context
/// without the tracker, so cross-frame reuse would be unsound. Within a
/// frame, every raw bind of a TRACKED slot elsewhere in the walk
/// invalidates it (the tracker only ever under-binds: a skipped bind is
/// always preceded by an identical tracked one on the same context
/// state).
///
/// VS and PS texture slots at the same register are tracked
/// SEPARATELY: the blur passes bind the primary texture at the pixel
/// stage only, so a shared slot would let a later vertex-stage bind
/// ride on a pixel-stage one (a real state change skipped).
struct BindTracker {
    /// The last-bound primitive topology (0 = D3D_PRIMITIVE_TOPOLOGY_UNDEFINED = nothing bound).
    topology: Cell<u32>,
    /// The last-bound viewport (x, y, w, h); None = invalidated.
    viewport: Cell<Option<[f32; 4]>>,
    /// The last-bound vertex shader (raw; null = none).
    vertex_shader: Cell<*mut std::ffi::c_void>,
    /// The last-bound pixel shader (raw; null = none).
    pixel_shader: Cell<*mut std::ffi::c_void>,
    /// The last-bound output-merger blend state (raw; null = none).
    blend_state: Cell<*mut std::ffi::c_void>,
    /// The b0/b1 cbuffer PAIR (the globals+font buffers, always bound
    /// together; the pair's identity is the globals buffer).
    cbuffer_pair: Cell<*mut std::ffi::c_void>,
    /// The per-draw data cbuffer (the instance pipelines' draw-constants
    /// or the blur params), the register it was bound at and the stages
    /// covered (the slot is register- and stage-checked: a bind at
    /// another register, or at a stage the cache does not cover, says
    /// nothing about the requested one).
    cbuffer_data: Cell<*mut std::ffi::c_void>,
    cbuffer_data_register: Cell<u32>,
    cbuffer_data_stages: Cell<u32>,
    /// The composite-extension cbuffer (b5, fixed register).
    cbuffer_extensions: Cell<*mut std::ffi::c_void>,
    /// The backdrop-crop cbuffer (b6, fixed register).
    cbuffer_crop: Cell<*mut std::ffi::c_void>,
    /// The primary sampler (s2, fixed register).
    primary_sampler: Cell<*mut std::ffi::c_void>,
    /// The primary texture's VERTEX-stage slot (t2).
    primary_srv_vs: Cell<*mut std::ffi::c_void>,
    /// The primary texture's PIXEL-stage slot (t2).
    primary_srv_ps: Cell<*mut std::ffi::c_void>,
    /// The instance-data SRV pair at the DATA register (t2; the batch
    /// loop binds it at both stages together).
    data_srv: Cell<*mut std::ffi::c_void>,
}

impl BindTracker {
    fn new() -> Self {
        BindTracker {
            topology: Cell::new(0),
            viewport: Cell::new(None),
            vertex_shader: Cell::new(std::ptr::null_mut()),
            pixel_shader: Cell::new(std::ptr::null_mut()),
            blend_state: Cell::new(std::ptr::null_mut()),
            cbuffer_pair: Cell::new(std::ptr::null_mut()),
            cbuffer_data: Cell::new(std::ptr::null_mut()),
            cbuffer_data_register: Cell::new(u32::MAX),
            cbuffer_data_stages: Cell::new(0),
            cbuffer_extensions: Cell::new(std::ptr::null_mut()),
            cbuffer_crop: Cell::new(std::ptr::null_mut()),
            primary_sampler: Cell::new(std::ptr::null_mut()),
            primary_srv_vs: Cell::new(std::ptr::null_mut()),
            primary_srv_ps: Cell::new(std::ptr::null_mut()),
            data_srv: Cell::new(std::ptr::null_mut()),
        }
    }

    /// Clears every cached slot (the raw-bind invalidation: a bind the
    /// tracker did not issue may have changed the context state).
    fn invalidate(&self) {
        self.topology.set(0);
        self.viewport.set(None);
        self.vertex_shader.set(std::ptr::null_mut());
        self.pixel_shader.set(std::ptr::null_mut());
        self.blend_state.set(std::ptr::null_mut());
        self.cbuffer_pair.set(std::ptr::null_mut());
        self.cbuffer_data.set(std::ptr::null_mut());
        self.cbuffer_data_register.set(u32::MAX);
        self.cbuffer_data_stages.set(0);
        self.cbuffer_extensions.set(std::ptr::null_mut());
        self.cbuffer_crop.set(std::ptr::null_mut());
        self.primary_sampler.set(std::ptr::null_mut());
        self.primary_srv_vs.set(std::ptr::null_mut());
        self.primary_srv_ps.set(std::ptr::null_mut());
        self.data_srv.set(std::ptr::null_mut());
    }

    fn count_bind() {
        STATE_BIND_COUNT.fetch_add(1, Ordering::Relaxed);
    }

    /// Binds the topology when it changed.
    fn bind_topology(&self, ctx: &ID3D11DeviceContext, topology: D3D_PRIMITIVE_TOPOLOGY) {
        if self.topology.get() != topology.0 as u32 {
            unsafe { ctx.IASetPrimitiveTopology(topology) };
            self.topology.set(topology.0 as u32);
            Self::count_bind();
        }
    }

    /// Binds the viewport when it changed.
    fn bind_viewport(&self, ctx: &ID3D11DeviceContext, viewport: &D3D11_VIEWPORT) {
        let rect = [
            viewport.TopLeftX,
            viewport.TopLeftY,
            viewport.Width,
            viewport.Height,
        ];
        if self.viewport.get() != Some(rect) {
            unsafe { ctx.RSSetViewports(Some(slice::from_ref(viewport))) };
            self.viewport.set(Some(rect));
            Self::count_bind();
        }
    }

    fn bind_vertex_shader(&self, ctx: &ID3D11DeviceContext, shader: &ID3D11VertexShader) {
        let raw = Interface::as_raw(shader);
        if self.vertex_shader.get() != raw {
            unsafe { ctx.VSSetShader(shader, None) };
            self.vertex_shader.set(raw);
            Self::count_bind();
        }
    }

    fn bind_pixel_shader(&self, ctx: &ID3D11DeviceContext, shader: &ID3D11PixelShader) {
        let raw = Interface::as_raw(shader);
        if self.pixel_shader.get() != raw {
            unsafe { ctx.PSSetShader(shader, None) };
            self.pixel_shader.set(raw);
            Self::count_bind();
        }
    }

    fn bind_blend_state(&self, ctx: &ID3D11DeviceContext, blend: &ID3D11BlendState) {
        let raw = Interface::as_raw(blend);
        if self.blend_state.get() != raw {
            unsafe { ctx.OMSetBlendState(blend, None, 0xFFFFFFFF) };
            self.blend_state.set(raw);
            Self::count_bind();
        }
    }

    /// Binds the globals+font cbuffer pair at register 0 (both stages
    /// together, the pair identified by the globals buffer).
    fn bind_cbuffer_pair(&self, ctx: &ID3D11DeviceContext, globals: &GlobalElements) {
        let raw = Interface::as_raw(&globals.globals_buffer);
        if self.cbuffer_pair.get() != raw {
            let cbuffers = globals.cbuffers();
            unsafe {
                ctx.VSSetConstantBuffers(0, Some(&cbuffers));
                ctx.PSSetConstantBuffers(0, Some(&cbuffers));
            }
            self.cbuffer_pair.set(raw);
            Self::count_bind();
        }
    }

    /// Binds one cbuffer at the DATA register (the draw-constants or
    /// the blur params), register- and stage-checked: the bind is
    /// skipped only when the cache covers the requested register,
    /// buffer AND stages.
    fn bind_cbuffer_data(
        &self,
        ctx: &ID3D11DeviceContext,
        register: u32,
        buffer: &ID3D11Buffer,
        stages: BindStages,
    ) {
        let raw = Interface::as_raw(buffer);
        let mask = stages as u32;
        let covered = self.cbuffer_data_register.get() == register
            && self.cbuffer_data.get() == raw
            && (self.cbuffer_data_stages.get() & mask) == mask;
        if !covered {
            let bound = [Some(buffer.clone())];
            unsafe {
                if stages != BindStages::Pixel {
                    ctx.VSSetConstantBuffers(register, Some(&bound));
                }
                if stages != BindStages::Vertex {
                    ctx.PSSetConstantBuffers(register, Some(&bound));
                }
            }
            self.cbuffer_data.set(raw);
            self.cbuffer_data_register.set(register);
            // The issued bind covers exactly the requested stages (the
            // other stage's slot keeps whatever it held).
            self.cbuffer_data_stages.set(mask);
            Self::count_bind();
        }
    }

    /// Binds the composite-extension cbuffer (b5, both stages).
    fn bind_cbuffer_extensions(&self, ctx: &ID3D11DeviceContext, buffer: &ID3D11Buffer) {
        let raw = Interface::as_raw(buffer);
        if self.cbuffer_extensions.get() != raw {
            let bound = [Some(buffer.clone())];
            unsafe {
                ctx.VSSetConstantBuffers(5, Some(&bound));
                ctx.PSSetConstantBuffers(5, Some(&bound));
            }
            self.cbuffer_extensions.set(raw);
            Self::count_bind();
        }
    }

    /// Binds the backdrop-crop cbuffer (b6, pixel stage).
    fn bind_cbuffer_crop(&self, ctx: &ID3D11DeviceContext, buffer: &ID3D11Buffer) {
        let raw = Interface::as_raw(buffer);
        if self.cbuffer_crop.get() != raw {
            let bound = [Some(buffer.clone())];
            unsafe { ctx.PSSetConstantBuffers(6, Some(&bound)) };
            self.cbuffer_crop.set(raw);
            Self::count_bind();
        }
    }

    fn bind_primary_sampler(&self, ctx: &ID3D11DeviceContext, sampler: &ID3D11SamplerState) {
        let raw = Interface::as_raw(sampler);
        if self.primary_sampler.get() != raw {
            let bound = [Some(sampler.clone())];
            unsafe { ctx.PSSetSamplers(PRIMARY_SAMPLER_REGISTER, Some(&bound)) };
            self.primary_sampler.set(raw);
            Self::count_bind();
        }
    }

    /// Binds the primary texture's SRV at the vertex stage.
    fn bind_primary_srv_vertex(
        &self,
        ctx: &ID3D11DeviceContext,
        srv: Option<&ID3D11ShaderResourceView>,
    ) {
        let raw = srv.map(Interface::as_raw).unwrap_or(std::ptr::null_mut());
        if self.primary_srv_vs.get() != raw {
            let bound = [srv.cloned()];
            unsafe { ctx.VSSetShaderResources(PRIMARY_TEXTURE_REGISTER, Some(&bound)) };
            self.primary_srv_vs.set(raw);
            Self::count_bind();
        }
    }

    /// Binds the primary texture's SRV at the pixel stage (None = the
    /// post-pass unbind).
    fn bind_primary_srv_pixel(
        &self,
        ctx: &ID3D11DeviceContext,
        srv: Option<&ID3D11ShaderResourceView>,
    ) {
        let raw = srv.map(Interface::as_raw).unwrap_or(std::ptr::null_mut());
        if self.primary_srv_ps.get() != raw {
            let bound = [srv.cloned()];
            unsafe { ctx.PSSetShaderResources(PRIMARY_TEXTURE_REGISTER, Some(&bound)) };
            self.primary_srv_ps.set(raw);
            Self::count_bind();
        }
    }

    /// Binds the instance-data SRV pair at the DATA register (both
    /// stages together).
    fn bind_data_srv(&self, ctx: &ID3D11DeviceContext, srv: Option<&ID3D11ShaderResourceView>) {
        let raw = srv.map(Interface::as_raw).unwrap_or(std::ptr::null_mut());
        if self.data_srv.get() != raw {
            let bound = [srv.cloned()];
            unsafe {
                ctx.VSSetShaderResources(DATA_REGISTER, Some(&bound));
                ctx.PSSetShaderResources(DATA_REGISTER, Some(&bound));
            }
            self.data_srv.set(raw);
            Self::count_bind();
        }
    }
}

/// The cbuffer stages a tracked bind covers.
#[derive(Clone, Copy, PartialEq)]
#[repr(u32)]
enum BindStages {
    Vertex = 1,
    Pixel = 2,
    Both = 3,
}

// ---------------------------------------------------------------------------
// Frame-wide bindings (the pin's FrameBindings)
// ---------------------------------------------------------------------------

/// Frame-wide state that every batch draw binds alongside its own
/// pipeline (the pin's `FrameBindings`, owning a clone of the global
/// elements so the draw walk can mutably update the instance buffers).
/// The state-bind tracker rides along (rev 16, ticket 53): every
/// tracked draw consults it, every raw bind outside the two tracked
/// paths invalidates it.
struct FrameBindings<'a> {
    device_context: &'a ID3D11DeviceContext,
    viewport: D3D11_VIEWPORT,
    globals: GlobalElements,
    /// The per-frame state-bind tracker (§3.12).
    binds: Rc<BindTracker>,
}

// ---------------------------------------------------------------------------
// D3D11 helpers (the pin's, verbatim)
// ---------------------------------------------------------------------------

fn create_vertex_shader(device: &ID3D11Device, bytes: &[u8]) -> Result<ID3D11VertexShader, i32> {
    unsafe {
        let mut shader = None;
        device
            .CreateVertexShader(bytes, None, Some(&mut shader))
            .map_err(|e| e.code().0)?;
        Ok(shader.ok_or(renderer_status::ERR_DEVICE)?)
    }
}

fn create_fragment_shader(device: &ID3D11Device, bytes: &[u8]) -> Result<ID3D11PixelShader, i32> {
    unsafe {
        let mut shader = None;
        device
            .CreatePixelShader(bytes, None, Some(&mut shader))
            .map_err(|e| e.code().0)?;
        Ok(shader.ok_or(renderer_status::ERR_DEVICE)?)
    }
}

/// The pin's `create_blend_state`: straight-alpha blending
/// (`SRC_ALPHA / INV_SRC_ALPHA`, alpha `ONE / ONE`).
fn create_blend_state(device: &ID3D11Device) -> Result<ID3D11BlendState, i32> {
    let mut desc = D3D11_BLEND_DESC::default();
    desc.RenderTarget[0].BlendEnable = true.into();
    desc.RenderTarget[0].BlendOp = D3D11_BLEND_OP_ADD;
    desc.RenderTarget[0].BlendOpAlpha = D3D11_BLEND_OP_ADD;
    desc.RenderTarget[0].SrcBlend = D3D11_BLEND_SRC_ALPHA;
    desc.RenderTarget[0].SrcBlendAlpha = D3D11_BLEND_ONE;
    desc.RenderTarget[0].DestBlend = D3D11_BLEND_INV_SRC_ALPHA;
    desc.RenderTarget[0].DestBlendAlpha = D3D11_BLEND_ONE;
    desc.RenderTarget[0].RenderTargetWriteMask = D3D11_COLOR_WRITE_ENABLE_ALL.0 as u8;
    unsafe {
        let mut state = None;
        device.CreateBlendState(&desc, Some(&mut state)).map_err(|e| e.code().0)?;
        Ok(state.ok_or(renderer_status::ERR_DEVICE)?)
    }
}

/// The pin's `create_blend_state_for_subpixel_rendering`: dual-source
/// color blending (`SRC1_COLOR / INV_SRC1_COLOR`), alpha
/// `ONE / ZERO`, and no writes to the alpha channel.
fn create_blend_state_for_subpixel_rendering(device: &ID3D11Device) -> Result<ID3D11BlendState, i32> {
    let mut desc = D3D11_BLEND_DESC::default();
    desc.RenderTarget[0].BlendEnable = true.into();
    desc.RenderTarget[0].BlendOp = D3D11_BLEND_OP_ADD;
    desc.RenderTarget[0].BlendOpAlpha = D3D11_BLEND_OP_ADD;
    desc.RenderTarget[0].SrcBlend = D3D11_BLEND_SRC1_COLOR;
    desc.RenderTarget[0].DestBlend = D3D11_BLEND_INV_SRC1_COLOR;
    // It does not make sense to draw transparent subpixel-rendered text, since it cannot be meaningfully alpha-blended onto anything else.
    desc.RenderTarget[0].SrcBlendAlpha = D3D11_BLEND_ONE;
    desc.RenderTarget[0].DestBlendAlpha = D3D11_BLEND_ZERO;
    desc.RenderTarget[0].RenderTargetWriteMask =
        D3D11_COLOR_WRITE_ENABLE_ALL.0 as u8 & !D3D11_COLOR_WRITE_ENABLE_ALPHA.0 as u8;
    unsafe {
        let mut state = None;
        device.CreateBlendState(&desc, Some(&mut state)).map_err(|e| e.code().0)?;
        Ok(state.ok_or(renderer_status::ERR_DEVICE)?)
    }
}

/// The pin's `create_blend_state_for_path_rasterization`: premultiplied
/// (`ONE / INV_SRC_ALPHA`).
fn create_blend_state_for_path_rasterization(device: &ID3D11Device) -> Result<ID3D11BlendState, i32> {
    let mut desc = D3D11_BLEND_DESC::default();
    desc.RenderTarget[0].BlendEnable = true.into();
    desc.RenderTarget[0].BlendOp = D3D11_BLEND_OP_ADD;
    desc.RenderTarget[0].BlendOpAlpha = D3D11_BLEND_OP_ADD;
    desc.RenderTarget[0].SrcBlend = D3D11_BLEND_ONE;
    desc.RenderTarget[0].SrcBlendAlpha = D3D11_BLEND_ONE;
    desc.RenderTarget[0].DestBlend = D3D11_BLEND_INV_SRC_ALPHA;
    desc.RenderTarget[0].DestBlendAlpha = D3D11_BLEND_INV_SRC_ALPHA;
    desc.RenderTarget[0].RenderTargetWriteMask = D3D11_COLOR_WRITE_ENABLE_ALL.0 as u8;
    unsafe {
        let mut state = None;
        device.CreateBlendState(&desc, Some(&mut state)).map_err(|e| e.code().0)?;
        Ok(state.ok_or(renderer_status::ERR_DEVICE)?)
    }
}

/// The pin's `create_blend_state_for_path_sprite`: premultiplied
/// compositing (`ONE / INV_SRC_ALPHA`, alpha `ONE / ONE`).
fn create_blend_state_for_path_sprite(device: &ID3D11Device) -> Result<ID3D11BlendState, i32> {
    let mut desc = D3D11_BLEND_DESC::default();
    desc.RenderTarget[0].BlendEnable = true.into();
    desc.RenderTarget[0].BlendOp = D3D11_BLEND_OP_ADD;
    desc.RenderTarget[0].BlendOpAlpha = D3D11_BLEND_OP_ADD;
    desc.RenderTarget[0].SrcBlend = D3D11_BLEND_ONE;
    desc.RenderTarget[0].SrcBlendAlpha = D3D11_BLEND_ONE;
    desc.RenderTarget[0].DestBlend = D3D11_BLEND_INV_SRC_ALPHA;
    desc.RenderTarget[0].DestBlendAlpha = D3D11_BLEND_ONE;
    desc.RenderTarget[0].RenderTargetWriteMask = D3D11_COLOR_WRITE_ENABLE_ALL.0 as u8;
    unsafe {
        let mut state = None;
        device.CreateBlendState(&desc, Some(&mut state)).map_err(|e| e.code().0)?;
        Ok(state.ok_or(renderer_status::ERR_DEVICE)?)
    }
}

/// A blend state that overwrites the target (no blending) — used for the
/// blur downsample and gaussian passes (the pin's
/// `create_blend_state_no_blend`).
fn create_blend_state_no_blend(device: &ID3D11Device) -> Result<ID3D11BlendState, i32> {
    let mut desc = D3D11_BLEND_DESC::default();
    desc.RenderTarget[0].BlendEnable = false.into();
    desc.RenderTarget[0].RenderTargetWriteMask = D3D11_COLOR_WRITE_ENABLE_ALL.0 as u8;
    unsafe {
        let mut state = None;
        device.CreateBlendState(&desc, Some(&mut state)).map_err(|e| e.code().0)?;
        Ok(state.ok_or(renderer_status::ERR_DEVICE)?)
    }
}

/// The pin's `create_constant_buffer`: a CPU-writable dynamic constant
/// buffer, byte size rounded up to 16.
fn create_constant_buffer(device: &ID3D11Device, byte_size: usize) -> Result<ID3D11Buffer, i32> {
    let desc = D3D11_BUFFER_DESC {
        ByteWidth: byte_size.next_multiple_of(16) as u32,
        Usage: D3D11_USAGE_DYNAMIC,
        BindFlags: D3D11_BIND_CONSTANT_BUFFER.0 as u32,
        CPUAccessFlags: D3D11_CPU_ACCESS_WRITE.0 as u32,
        ..Default::default()
    };
    let mut buffer = None;
    unsafe {
        device.CreateBuffer(&desc, None, Some(&mut buffer)).map_err(|e| e.code().0)?;
    }
    Ok(buffer.ok_or(renderer_status::ERR_DEVICE)?)
}

/// The pin's `create_buffer` + `create_buffer_view`: a dynamic
/// raw-view-enabled buffer (the HLSL reads instances through a raw
/// `ByteAddressBuffer` view) with 4-byte-aligned contents.
fn create_instance_buffer(
    device: &ID3D11Device,
    element_size: usize,
    capacity: usize,
) -> Result<(ID3D11Buffer, Option<ID3D11ShaderResourceView>), i32> {
    if element_size == 0 {
        return Err(renderer_status::ERR_DEVICE);
    }
    let byte_width = element_size
        .checked_mul(capacity)
        .ok_or(renderer_status::ERR_BAD_VALUE)?;
    if byte_width > u32::MAX as usize {
        return Err(renderer_status::ERR_CAPACITY);
    }
    if byte_width % 4 != 0 {
        return Err(renderer_status::ERR_BAD_VALUE);
    }
    let desc = D3D11_BUFFER_DESC {
        ByteWidth: byte_width as u32,
        Usage: D3D11_USAGE_DYNAMIC,
        BindFlags: D3D11_BIND_SHADER_RESOURCE.0 as u32,
        CPUAccessFlags: D3D11_CPU_ACCESS_WRITE.0 as u32,
        MiscFlags: D3D11_RESOURCE_MISC_BUFFER_ALLOW_RAW_VIEWS.0 as u32,
        ..Default::default()
    };
    let mut buffer = None;
    unsafe {
        device.CreateBuffer(&desc, None, Some(&mut buffer)).map_err(|e| e.code().0)?;
    }
    let buffer = buffer.ok_or(renderer_status::ERR_DEVICE)?;
    let view = create_buffer_view(device, &buffer)?;
    Ok((buffer, view))
}

fn create_buffer_view(
    device: &ID3D11Device,
    buffer: &ID3D11Buffer,
) -> Result<Option<ID3D11ShaderResourceView>, i32> {
    let mut buffer_desc = D3D11_BUFFER_DESC::default();
    unsafe { buffer.GetDesc(&mut buffer_desc) };
    let desc = D3D11_SHADER_RESOURCE_VIEW_DESC {
        Format: DXGI_FORMAT_R32_TYPELESS,
        ViewDimension: D3D11_SRV_DIMENSION_BUFFEREX,
        Anonymous: D3D11_SHADER_RESOURCE_VIEW_DESC_0 {
            BufferEx: D3D11_BUFFEREX_SRV {
                FirstElement: 0,
                NumElements: buffer_desc.ByteWidth / 4,
                Flags: D3D11_BUFFEREX_SRV_FLAG_RAW.0 as u32,
            },
        },
    };
    let mut view = None;
    unsafe {
        device
            .CreateShaderResourceView(buffer, Some(&desc), Some(&mut view))
            .map_err(|e| e.code().0)?;
    }
    Ok(view)
}

/// The pin's `update_buffer`: `MAP_WRITE_DISCARD` + copy + unmap.
fn update_buffer<T>(
    device_context: &ID3D11DeviceContext,
    buffer: &ID3D11Buffer,
    data: &[T],
) -> Result<(), i32> {
    unsafe {
        let mut dest = std::mem::zeroed();
        device_context
            .Map(buffer, 0, D3D11_MAP_WRITE_DISCARD, 0, Some(&mut dest))
            .map_err(|e| e.code().0)?;
        std::ptr::copy_nonoverlapping(data.as_ptr(), dest.pData as _, data.len());
        device_context.Unmap(buffer, 0);
    }
    Ok(())
}

/// The pin's `create_path_intermediate_texture`: a color texture usable
/// as both a render target and a shader resource, returning both views.
fn create_color_target(
    device: &ID3D11Device,
    width: u32,
    height: u32,
) -> Result<
    (
        ID3D11Texture2D,
        Option<ID3D11RenderTargetView>,
        Option<ID3D11ShaderResourceView>,
    ),
    i32,
> {
    let texture = unsafe {
        let mut output = None;
        let desc = D3D11_TEXTURE2D_DESC {
            Width: width.max(1),
            Height: height.max(1),
            MipLevels: 1,
            ArraySize: 1,
            Format: DXGI_FORMAT_B8G8R8A8_UNORM,
            SampleDesc: DXGI_SAMPLE_DESC {
                Count: 1,
                Quality: 0,
            },
            Usage: D3D11_USAGE_DEFAULT,
            BindFlags: (D3D11_BIND_RENDER_TARGET.0 | D3D11_BIND_SHADER_RESOURCE.0) as u32,
            CPUAccessFlags: 0,
            MiscFlags: 0,
        };
        device.CreateTexture2D(&desc, None, Some(&mut output)).map_err(|e| e.code().0)?;
        output.ok_or(renderer_status::ERR_DEVICE)?
    };
    let mut rtv = None;
    unsafe {
        device
            .CreateRenderTargetView(&texture, None, Some(&mut rtv))
            .map_err(|e| e.code().0)?;
    }
    let mut srv = None;
    unsafe {
        device
            .CreateShaderResourceView(&texture, None, Some(&mut srv))
            .map_err(|e| e.code().0)?;
    }
    Ok((texture, rtv, srv))
}

fn create_path_intermediate_texture(
    device: &ID3D11Device,
    width: u32,
    height: u32,
) -> Result<(ID3D11Texture2D, Option<ID3D11ShaderResourceView>), i32> {
    let texture = unsafe {
        let mut output = None;
        let desc = D3D11_TEXTURE2D_DESC {
            Width: width,
            Height: height,
            MipLevels: 1,
            ArraySize: 1,
            Format: DXGI_FORMAT_B8G8R8A8_UNORM,
            SampleDesc: DXGI_SAMPLE_DESC {
                Count: 1,
                Quality: 0,
            },
            Usage: D3D11_USAGE_DEFAULT,
            BindFlags: (D3D11_BIND_RENDER_TARGET.0 | D3D11_BIND_SHADER_RESOURCE.0) as u32,
            CPUAccessFlags: 0,
            MiscFlags: 0,
        };
        device.CreateTexture2D(&desc, None, Some(&mut output)).map_err(|e| e.code().0)?;
        output.ok_or(renderer_status::ERR_DEVICE)?
    };
    let mut srv = None;
    unsafe {
        device
            .CreateShaderResourceView(&texture, None, Some(&mut srv))
            .map_err(|e| e.code().0)?;
    }
    Ok((texture, srv))
}

fn create_path_intermediate_msaa_texture_and_view(
    device: &ID3D11Device,
    width: u32,
    height: u32,
) -> Result<(ID3D11Texture2D, Option<ID3D11RenderTargetView>), i32> {
    let msaa_texture = unsafe {
        let mut output = None;
        let desc = D3D11_TEXTURE2D_DESC {
            Width: width,
            Height: height,
            MipLevels: 1,
            ArraySize: 1,
            Format: DXGI_FORMAT_B8G8R8A8_UNORM,
            SampleDesc: DXGI_SAMPLE_DESC {
                Count: PATH_MULTISAMPLE_COUNT,
                Quality: D3D11_STANDARD_MULTISAMPLE_PATTERN.0 as u32,
            },
            Usage: D3D11_USAGE_DEFAULT,
            BindFlags: D3D11_BIND_RENDER_TARGET.0 as u32,
            CPUAccessFlags: 0,
            MiscFlags: 0,
        };
        device.CreateTexture2D(&desc, None, Some(&mut output)).map_err(|e| e.code().0)?;
        output.ok_or(renderer_status::ERR_DEVICE)?
    };
    let mut msaa_view = None;
    unsafe {
        device
            .CreateRenderTargetView(&msaa_texture, None, Some(&mut msaa_view))
            .map_err(|e| e.code().0)?;
    }
    Ok((msaa_texture, msaa_view))
}

/// The gradient instance buffer's element stride (rev 16, ticket 52):
/// the ABI record's 56 bytes reshaped for the HLSL structured-buffer
/// layout - the two `vec4f` geometry fields land 16-aligned and the
/// struct's size rounds up to its 16-byte alignment (a 64-byte stride;
/// the trailing two words are padding).
const GRADIENT_INSTANCE_STRIDE: usize = 64;

/// Creates the gradient ramp texture (rev 16, ticket 52): a DYNAMIC
/// 1024-wide R8G8B8A8_UNORM texture of `rows` rows, CPU-writable and
/// GPU-readable (the upload path maps it once per frame and copies
/// the scene's baked ramp rows; unused rows are cleared).
fn create_gradient_ramp_texture(
    device: &ID3D11Device,
    rows: u32,
) -> Result<(ID3D11Texture2D, Option<ID3D11ShaderResourceView>), i32> {
    let texture = unsafe {
        let mut output = None;
        let desc = D3D11_TEXTURE2D_DESC {
            Width: crate::scene::GRADIENT_RAMP_WIDTH,
            Height: rows.max(1),
            MipLevels: 1,
            ArraySize: 1,
            Format: DXGI_FORMAT_R8G8B8A8_UNORM,
            SampleDesc: DXGI_SAMPLE_DESC { Count: 1, Quality: 0 },
            Usage: D3D11_USAGE_DYNAMIC,
            BindFlags: D3D11_BIND_SHADER_RESOURCE.0 as u32,
            CPUAccessFlags: D3D11_CPU_ACCESS_WRITE.0 as u32,
            MiscFlags: 0,
        };
        device.CreateTexture2D(&desc, None, Some(&mut output)).map_err(|e| e.code().0)?;
        output.ok_or(renderer_status::ERR_DEVICE)?
    };
    let mut srv = None;
    unsafe {
        device
            .CreateShaderResourceView(&texture, None, Some(&mut srv))
            .map_err(|e| e.code().0)?;
    }
    Ok((texture, srv))
}

/// The clip-path COVERAGE resources (rev 16, ticket 52): the window-
/// sized MSAA rasterization target, the resolved coverage texture and
/// its SRV - the pinned path machinery's shape, rasterized with a
/// solid white fill so the resolved texture's channels carry the
/// path's coverage (the mask composite samples it as the A8 source).
#[derive(Default)]
pub(super) struct CoverageResources {
    texture: Option<ID3D11Texture2D>,
    srv: Option<ID3D11ShaderResourceView>,
    msaa_texture: Option<ID3D11Texture2D>,
    msaa_view: Option<ID3D11RenderTargetView>,
}

impl CoverageResources {
    fn new(device: &ID3D11Device, width: u32, height: u32) -> Result<Self, i32> {
        let (texture, srv) = create_path_intermediate_texture(device, width, height)?;
        let (msaa_texture, msaa_view) =
            create_path_intermediate_msaa_texture_and_view(device, width, height)?;
        Ok(Self {
            texture: Some(texture),
            srv,
            msaa_texture: Some(msaa_texture),
            msaa_view,
        })
    }
}

/// The blend composite's backdrop COPY target (rev 16, ticket 52): one
/// window-sized color texture the composite's prep pass fills with the
/// parent target's content (the D3D11 shader-pass copy - the parent's
/// SRV sampled 1:1), so the blend fragment can read the backdrop while
/// writing the parent (no read-write hazard). Recreated on resize.
#[derive(Default)]
pub(super) struct BlendBackdropTarget {
    rtv: Option<ID3D11RenderTargetView>,
    srv: Option<ID3D11ShaderResourceView>,
}

impl BlendBackdropTarget {
    fn new(device: &ID3D11Device, width: u32, height: u32) -> Result<Self, i32> {
        let (_, rtv, srv) = create_color_target(device, width, height)?;
        Ok(Self { rtv, srv })
    }
}

/// The pin's `set_rasterizer_state` (solid fill, no culling, MSAA line
/// rasterization enabled).
fn set_rasterizer_state(device: &ID3D11Device, device_context: &ID3D11DeviceContext) -> Result<(), i32> {
    let desc = D3D11_RASTERIZER_DESC {
        FillMode: D3D11_FILL_SOLID,
        CullMode: D3D11_CULL_NONE,
        FrontCounterClockwise: false.into(),
        DepthBias: 0,
        DepthBiasClamp: 0.0,
        SlopeScaledDepthBias: 0.0,
        DepthClipEnable: true.into(),
        ScissorEnable: false.into(),
        MultisampleEnable: true.into(),
        AntialiasedLineEnable: false.into(),
    };
    let rasterizer_state = unsafe {
        let mut state = None;
        device.CreateRasterizerState(&desc, Some(&mut state)).map_err(|e| e.code().0)?;
        state.ok_or(renderer_status::ERR_DEVICE)?
    };
    unsafe { device_context.RSSetState(&rasterizer_state) };
    Ok(())
}

// ---------------------------------------------------------------------------
// The draw-global lease (panic-safe take/put-back of the pipeline set)
// ---------------------------------------------------------------------------

/// Takes the device-global draw state out of the renderer state for the
/// duration of a draw, restoring it on drop (including during unwind —
/// a contained panic must not leave the service without its pipelines).
struct DrawLease<'a> {
    state: &'a mut RendererState,
    value: Option<DrawGlobal>,
}

impl<'a> DrawLease<'a> {
    fn take(state: &'a mut RendererState) -> Result<Self, i32> {
        if state.draw.is_none() {
            return Err(renderer_status::ERR_DEVICE);
        }
        let value = state.draw.take();
        Ok(DrawLease { state, value })
    }

}

impl Drop for DrawLease<'_> {
    fn drop(&mut self) {
        if let Some(value) = self.value.take() {
            self.state.draw = Some(value);
        }
    }
}

/// Creates the device-global draw state on first use (the pipelines and
/// the global elements; also sets the pin's rasterizer state on the
/// shared context — the pin does this at renderer construction).
pub(super) fn ensure_draw_global(state: &mut RendererState) -> Result<(), i32> {
    if state.draw.is_some() {
        return Ok(());
    }
    let device_shared = state.device.as_ref().ok_or(renderer_status::ERR_DEVICE)?;
    let device = device_shared.device.clone();
    let context = device_shared.context.clone();
    let globals = GlobalElements::new(&device)?;
    let pipelines = DrawPipelines::new(&device)?;
    set_rasterizer_state(&device, &context)?;
    state.draw = Some(DrawGlobal {
        globals,
        pipelines,
        surface_views: std::collections::HashMap::new(),
    });
    Ok(())
}

// ---------------------------------------------------------------------------
// The render walk (the pin's DirectXRenderer::render)
// ---------------------------------------------------------------------------

/// The pin's `pre_draw`: upload the global and font uniforms, clear the
/// render target with the appearance's clear color, bind target and
/// viewport.
fn pre_draw(
    ctx: &ID3D11DeviceContext,
    globals: &GlobalElements,
    viewport: &D3D11_VIEWPORT,
    rtv: &Option<ID3D11RenderTargetView>,
    appearance: u32,
) -> Result<(), i32> {
    let clear_color: [f32; 4] = if appearance == background_appearance::OPAQUE {
        [1.0; 4]
    } else {
        [0.0; 4]
    };
    let font = font_info();
    update_buffer(
        ctx,
        &globals.globals_buffer,
        &[GlobalUniforms {
            viewport_size: vec2f(viewport.Width, viewport.Height),
            // DirectComposition wants premultiplied output, but path
            // rasterization premultiplies in-shader; scene geometry
            // blends straight alpha as before.
            premultiplied_alpha: gpui_render::shaders::common::ShaderBool::Disabled,
            padding: 0,
        }],
    )?;
    update_buffer(
        ctx,
        &globals.font_buffer,
        &[FontRasterizationUniforms {
            gamma_ratios: vec4f(
                font.gamma_ratios[0],
                font.gamma_ratios[1],
                font.gamma_ratios[2],
                font.gamma_ratios[3],
            ),
            grayscale_enhanced_contrast: font.grayscale_enhanced_contrast,
            subpixel_enhanced_contrast: font.subpixel_enhanced_contrast,
            uses_blue_green_red_subpixel_order:
                gpui_render::shaders::common::ShaderBool::from(font.is_bgr),
            padding: 0,
        }],
    )?;
    let rtv = rtv.as_ref().ok_or(renderer_status::ERR_DEVICE)?;
    unsafe {
        ctx.ClearRenderTargetView(rtv, &clear_color);
        let bound = Some(rtv.clone());
        ctx.OMSetRenderTargets(Some(slice::from_ref(&bound)), None);
        ctx.RSSetViewports(Some(slice::from_ref(viewport)));
    }
    Ok(())
}

/// Uploads the globals' viewport size for the CURRENT render target
/// (rev 16, ticket 51): the vertex clip math divides by it, so a pooled
/// group bucket needs its own size while the walk is inside it (the
/// pin's `pre_draw` does the same for the window; the font uniforms are
/// untouched - they never depend on the target).
fn set_target_globals(
    ctx: &ID3D11DeviceContext,
    globals: &GlobalElements,
    size: [f32; 2],
) -> Result<(), i32> {
    update_buffer(
        ctx,
        &globals.globals_buffer,
        &[GlobalUniforms {
            viewport_size: vec2f(size[0], size[1]),
            premultiplied_alpha: gpui_render::shaders::common::ShaderBool::Disabled,
            padding: 0,
        }],
    )
}

/// One draw pass over a finished scene's render plan (the pin's
/// `render`): uploads the frame's instance buffers, walks the compiled
/// command stream in order, and manages the offscene/isolated group
/// targets. The caller holds the renderer-global lock and the draw
/// lease.
fn render_scene(
    state: &mut RendererState,
    draw: &mut DrawGlobal,
    slot_index: usize,
    slot_generation: u32,
    snapshot: &Snapshot,
    atlas: u64,
    appearance: u32,
) -> Result<(), i32> {
    let device_shared = state.device.as_ref().ok_or(renderer_status::ERR_DEVICE)?;
    let device = device_shared.device.clone();
    let ctx = device_shared.context.clone();
    let surface_state = resolve_slot_mut(state, slot_index, slot_generation)?;
    let swapchain_rtv = surface_state.render_target_view.clone();
    let viewport = surface_state.viewport;
    let window = (surface_state.width, surface_state.height);

    pre_draw(&ctx, &draw.globals, &viewport, &swapchain_rtv, appearance)?;
    upload_scene_buffers(&device, &ctx, draw, snapshot)?;

    // Only route through the offscreen scene texture when the scene
    // contains backdrop filters or content-filter groups (R15's rule);
    // effect groups render through the pooled bucket targets directly,
    // so a group-only scene stays on the swapchain path.
    let use_offscreen = snapshot.requirements.uses_offscreen_target != 0;
    if snapshot.requirements.uses_path_target != 0 {
        ensure_path_resources(state, slot_index, slot_generation, &device)?;
    }
    if use_offscreen {
        ensure_blur_resources(state, slot_index, slot_generation, &device)?;
    }
    // rev 16 (ticket 52): the clip-path coverage rasterization and the
    // blend backdrop copy target, created when the scene carries them.
    let scene_has_coverage = snapshot
        .effects
        .iter()
        .any(|effect| effect.mask_kind == 3);
    if scene_has_coverage && !snapshot.coverage.is_empty() {
        ensure_coverage_resources(state, slot_index, slot_generation, &device)?;
    }
    let scene_has_blend = snapshot
        .effects
        .iter()
        .any(|effect| (effect.flags & crate::scene::effect_flags::HAS_BLEND) != 0);
    if scene_has_blend {
        ensure_blend_backdrop(state, slot_index, slot_generation, &device)?;
    }

    let (scene_rtv, scene_srv) = {
        let surface_state = resolve_slot(state, slot_index, slot_generation)?;
        match surface_state.draw_targets.blur.as_ref() {
            Some(blur) => (blur.scene_color_rtv.clone(), blur.scene_color_srv.clone()),
            None => {
                if use_offscreen {
                    return Err(renderer_status::ERR_DEVICE);
                }
                (None, None)
            }
        }
    };

    // The pre-pass over the command stream: every ISOLATED effect
    // group's content extent (the union of its span's primitive bounds
    // and its nested groups' composite extents - the bucket and the
    // composite quad for a group that does not clip).
    let group_extents = group_content_extents(snapshot);

    let mut active_render_target: Option<ID3D11RenderTargetView>;
    let base = ActiveTarget {
        rtv: if use_offscreen {
            scene_rtv.clone()
        } else {
            swapchain_rtv.clone()
        },
        srv: if use_offscreen { scene_srv.clone() } else { None },
        viewport,
        origin: [0.0, 0.0],
        size: [window.0 as f32, window.1 as f32],
    };
    if use_offscreen {
        unsafe {
            if let Some(rtv) = scene_rtv.as_ref() {
                ctx.ClearRenderTargetView(rtv, &[0.0; 4]);
                let bound = Some(rtv.clone());
                ctx.OMSetRenderTargets(Some(slice::from_ref(&bound)), None);
            }
        }
        active_render_target = scene_rtv.clone();
    } else {
        active_render_target = swapchain_rtv.clone();
    }

    let mut current = base.clone();

    // rev 16 (ticket 52): the clip-path coverage rasterization runs
    // BEFORE the command walk (the composites sample it), in the BASE
    // frame - the window's coordinates and globals (a transform
    // group's local-frame mask is the documented limit: the coverage
    // rasterizes at the window frame).
    let coverage_rasterized = scene_has_coverage && !snapshot.coverage.is_empty();

    // The shared group stack (rev 16, ticket 50): one entry per open
    // group of EITHER kind - the parent target to restore and the
    // pooled lease (None when the group renders inline: an inline
    // command from the plan, or the pool budget demoted an isolated
    // group). The END arm reads the kind from its own command.
    //
    // rev 16 (ticket 51): `tag` matches the END command's
    // boundary_index (the BEGIN's effect-record index; usize::MAX for
    // filter frames) so an INLINE effect end can restore exactly the
    // frame its begin pushed, and `offset` carries the inline
    // translation fast path (the viewport offset when the group's
    // transform is translation-only and nothing else groups).
    struct GroupFrame {
        parent: ActiveTarget,
        lease: Option<GroupLease>,
        tag: usize,
        /// The group's content region in the CURRENT frame (the
        /// bucket's un-quantized extent: the clip bounds or the
        /// content-extent/record-bounds region the BEGIN looked up).
        /// The composites draw THIS rect (not the quantized bucket),
        /// sampling the bucket at (region-relative position) / bucket
        /// size - the quantization margin never reaches the target.
        region: [f32; 4],
    }
    let mut group_stack: Vec<GroupFrame> = Vec::new();

    let mut frame = FrameBindings {
        device_context: &ctx,
        viewport,
        globals: draw.globals.clone(),
        binds: Rc::new(BindTracker::new()),
    };
    // `pre_draw` bound the viewport and the target outside the tracker
    // (it takes no frame): a fresh per-frame tracker is empty anyway,
    // and the invalidation documents the rule — every raw bind of a
    // tracked slot resets the cache (§3.12).
    frame.binds.invalidate();

    // rev 16 (ticket 52): the clip-path coverage rasterization (see the
    // comment above) - the composites sample the resolved target.
    if coverage_rasterized {
        rasterize_coverage(state, draw, slot_index, slot_generation, &frame, snapshot)?;
        // Restore the base target binding (the rasterization drew into
        // the coverage MSAA target; the raw viewport bind invalidates
        // the tracker - §3.12).
        unsafe {
            ctx.OMSetRenderTargets(Some(slice::from_ref(&current.rtv)), None);
            ctx.RSSetViewports(Some(slice::from_ref(&current.viewport)));
        }
        frame.binds.invalidate();
    }

    let mut path_rasterization_vertices: Vec<PathRasterizationVertex> = Vec::new();
    let mut path_sprites: Vec<PathSprite> = Vec::new();

    for command in &snapshot.commands {
        if command.command_kind == command_kind::BATCH {
            let range = command.range_start as usize..command.range_end as usize;
            match command.primitive_kind {
                batch_primitive_kind::SHADOWS => {
                    let instances = instance_range(command.range_start, command.range_end)?;
                    bind_gradient_resources(&ctx, draw);
                    draw.pipelines.shadow_pipeline.draw_instances_variant(
                        &frame,
                        None,
                        instances.first(),
                        instances.count(),
                        command.smoothed != 0,
                        false,
                    )?;
                }
                batch_primitive_kind::QUADS => {
                    let instances = instance_range(command.range_start, command.range_end)?;
                    bind_gradient_resources(&ctx, draw);
                    draw.pipelines.quad_pipeline.draw_instances_variant(
                        &frame,
                        None,
                        instances.first(),
                        instances.count(),
                        command.smoothed != 0,
                        false,
                    )?;
                }
                batch_primitive_kind::PATHS => {
                    if command.rasterization_vertex_count == 0 {
                        continue;
                    }
                    let paths = &snapshot.paths[range.clone()];
                    draw_paths_to_intermediate(
                        state,
                        draw,
                        slot_index,
                        slot_generation,
                        &frame,
                        paths,
                        command.rasterization_vertex_count as usize,
                        &mut path_rasterization_vertices,
                        &active_render_target,
                        &swapchain_rtv,
                    )?;
                    draw_paths_from_intermediate(
                        state,
                        draw,
                        slot_index,
                        slot_generation,
                        &frame,
                        paths,
                        command.sprite_count as usize,
                        &mut path_sprites,
                    )?;
                }
                batch_primitive_kind::UNDERLINES => {
                    let instances = instance_range(command.range_start, command.range_end)?;
                    draw.pipelines.underline_pipeline.draw_instances(
                        &frame,
                        None,
                        instances.first(),
                        instances.count(),
                    )?;
                }
                batch_primitive_kind::MONOCHROME_SPRITES => {
                    let texture = sprite_texture(atlas, 0, command.texture_index)?;
                    let instances = instance_range(command.range_start, command.range_end)?;
                    draw.pipelines.mono_sprites.draw_instances_sampling(
                        &frame,
                        Some(&texture),
                        instances.first(),
                        instances.count(),
                        command.pixelated != 0,
                    )?;
                }
                batch_primitive_kind::SUBPIXEL_SPRITES => {
                    let texture = sprite_texture(atlas, 2, command.texture_index)?;
                    let instances = instance_range(command.range_start, command.range_end)?;
                    draw.pipelines.subpixel_sprites.draw_instances_sampling(
                        &frame,
                        Some(&texture),
                        instances.first(),
                        instances.count(),
                        command.pixelated != 0,
                    )?;
                }
                batch_primitive_kind::POLYCHROME_SPRITES => {
                    let texture = sprite_texture(atlas, 1, command.texture_index)?;
                    let instances = instance_range(command.range_start, command.range_end)?;
                    draw.pipelines.poly_sprites.draw_instances_variant(
                        &frame,
                        Some(&texture),
                        instances.first(),
                        instances.count(),
                        command.smoothed != 0,
                        command.pixelated != 0,
                    )?;
                }
                batch_primitive_kind::SURFACES => {
                    // The current target's viewport (a group bucket's
                    // offset viewport when the surface draws inside an
                    // open group; the surface viewport otherwise).
                    draw_surfaces(
                        state,
                        draw,
                        &frame,
                        &frame.viewport,
                        &snapshot.surfaces[range.clone()],
                        &snapshot.surface_opacities[range.clone()],
                    )?;
                    // Restore the current target for subsequent batches
                    // (the pinned loop's post-draw rebinding).
                    unsafe {
                        ctx.OMSetRenderTargets(Some(slice::from_ref(&current.rtv)), None);
                    }
                }
                batch_primitive_kind::BACKDROP_FILTERS => {
                    for filter in &snapshot.backdrops[range.clone()] {
                        dx_blur_and_composite(
                            state,
                            draw,
                            slot_index,
                            slot_generation,
                            &frame,
                            &current,
                            filter,
                            // Backdrops clip to the rounded rect and
                            // crop their input to the element (rev 15),
                            // drawing into the CURRENT target.
                            true,
                        )?;
                    }
                    // Restore the current target for subsequent batches.
                    unsafe {
                        ctx.OMSetRenderTargets(Some(slice::from_ref(&current.rtv)), None);
                    }
                }
                _ => {
                    // The marker tags never appear as a batch command
                    // (the plan compiles them into begin/end commands).
                }
            }
        } else if command.command_kind == command_kind::BEGIN_FILTER
            || command.command_kind == command_kind::BEGIN_EFFECT
        {
            // An ISOLATED group begins: acquire a pooled bucket for its
            // content, redirect the walk into it, and clear it. A pool
            // overflow demotes the group to inline (the children keep
            // drawing into the current target, un-grouped - observably:
            // the debug-stats counter and a debug-build warning already
            // fired in the pool).
            //
            // rev 16 (ticket 51, fixing 50's handed-off gap): the bucket
            // renders at its FRAME'S ORIGIN with the GLOBALS' viewport
            // size set to the bucket - the children keep their true
            // coordinates, so every pipeline's fragment invariant
            // (SV_Position equals the records' coordinate space - the
            // polychrome sprite's rounded-rect SDF, the vendored
            // quads'/shadows' border geometry, the underline wave, the
            // content-mask fades) holds exactly as it does on the
            // window. The offset-viewport shape 50 sketched (window
            // coordinates shifted by the bucket origin) breaks that
            // invariant: the fragment's SV_Position lands at
            // window-minus-origin while the records stay window-space,
            // and the per-pixel geometry (measured against the records)
            // reads the sprite/rounded-quad as fully outside - nothing
            // draws. The bucket therefore spans the frame's [0,
            // extent max]: bounds-sized (quantized to the 256-px
            // granularity, clamped to the window) for effect groups -
            // the local extent for a transform group's local-authored
            // subtree, the content extent's max corner for window-frame
            // groups - and window-sized for FILTER groups (R15's exact
            // blur textures and scratch: a bounds-sized filter bucket
            // would wrap the gaussian at the region edge and shift the
            // R15 blur fixtures' edge pixels; ticket 53's region-limited
            // blur owns that change).
            let is_filter = command.command_kind == command_kind::BEGIN_FILTER;
            if command.filter_target == 1 {
                let record_bounds = if is_filter {
                    snapshot
                        .boundaries
                        .get(command.boundary_index as usize)
                        .map(|record| (record.bounds, record.max_blur_radius()))
                        .ok_or(renderer_status::ERR_BAD_VALUE)?
                } else {
                    snapshot
                        .effects
                        .get(command.boundary_index as usize)
                        .map(|record| (record.bounds, 0.0))
                        .ok_or(renderer_status::ERR_BAD_VALUE)?
                };
                let record = record_bounds.0;
                let blur_radius = record_bounds.1;
                // The group's content region in the CURRENT frame: the
                // dilated record bounds for filter groups (R15's exact
                // composite quad); the clip bounds when the effect
                // group clips (content outside the rrect is dropped -
                // CSS overflow); the content extent when it does not
                // (nothing is lost). A transform group's region is its
                // LOCAL content extent (the subtree's records are
                // local-frame).
                let clip_to_bounds = !is_filter
                    && (snapshot.effects[command.boundary_index as usize].flags
                        & (crate::scene::effect_flags::CLIP_TO_BOUNDS
                            | crate::scene::effect_flags::ROUNDED_CLIP_ONLY))
                        != 0;
                let region = if is_filter {
                    let dilation = GAUSSIAN_CUTOFF_STANDARD_DEVIATIONS * blur_radius;
                    [
                        f32::from_bits(record.x) - dilation,
                        f32::from_bits(record.y) - dilation,
                        f32::from_bits(record.w) + 2.0 * dilation,
                        f32::from_bits(record.h) + 2.0 * dilation,
                    ]
                } else if clip_to_bounds {
                    [
                        f32::from_bits(record.x),
                        f32::from_bits(record.y),
                        f32::from_bits(record.w),
                        f32::from_bits(record.h),
                    ]
                } else {
                    group_extents
                        .iter()
                        .find(|(index, ..)| *index == command_index(snapshot, command))
                        .map(|(_, region, _)| *region)
                        .unwrap_or([
                            f32::from_bits(record.x),
                            f32::from_bits(record.y),
                            f32::from_bits(record.w),
                            f32::from_bits(record.h),
                        ])
                };
                // The bucket's extent (the frame's [0, max]): the
                // window for filter groups (R15-exact); the region's
                // far corner otherwise (quantized up, clamped to the
                // window).
                let bucket_extent = if is_filter {
                    [window.0 as f32, window.1 as f32]
                } else {
                    [region[0] + region[2], region[1] + region[3]]
                };
                let needs_half = is_filter && BlurKernel::for_radius(blur_radius).is_some();
                let lease = {
                    let surface_state = resolve_slot_mut(state, slot_index, slot_generation)?;
                    surface_state.draw_targets.group_pool.acquire(
                        &device,
                        [0.0, 0.0],
                        [bucket_extent[0], bucket_extent[1]],
                        window,
                        needs_half,
                    )
                };
                match lease {
                    Some(lease) => {
                        group_stack.push(GroupFrame {
                            parent: current.clone(),
                            lease: Some(lease.clone()),
                            tag: command.boundary_index as usize,
                            region,
                        });
                        current = ActiveTarget {
                            rtv: lease.rtv.clone(),
                            srv: lease.srv.clone(),
                            // The bucket renders the frame's [0, size]:
                            // the children draw at their true frame
                            // coordinates (pixels beyond the bucket are
                            // dropped - exactly the content the
                            // composite never draws).
                            viewport: D3D11_VIEWPORT {
                                TopLeftX: 0.0,
                                TopLeftY: 0.0,
                                Width: lease.size[0],
                                Height: lease.size[1],
                                MinDepth: 0.0,
                                MaxDepth: 1.0,
                            },
                            origin: [0.0, 0.0],
                            size: lease.size,
                        };
                        active_render_target = current.rtv.clone();
                        frame.viewport = current.viewport;
                        // The globals' viewport size must match the
                        // bound viewport: the vertex clip math divides
                        // by it, so the children's frame coordinates map
                        // to the bucket pixel-for-pixel (the pin's own
                        // invariant - its pre_draw does exactly this
                        // for the window).
                        set_target_globals(&ctx, &frame.globals, current.size)?;
                        unsafe {
                            if let Some(rtv) = current.rtv.as_ref() {
                                ctx.ClearRenderTargetView(rtv, &[0.0; 4]);
                                let bound = Some(rtv.clone());
                                ctx.OMSetRenderTargets(Some(slice::from_ref(&bound)), None);
                                ctx.RSSetViewports(Some(slice::from_ref(&current.viewport)));
                            }
                        }
                        // The raw viewport bind invalidates the tracker
                        // (§3.12).
                        frame.binds.invalidate();
                    }
                    None => {
                        // The pool demoted the group to inline: no target
                        // switch, nothing to clear; the end command is a
                        // no-op for the walk (the overflow is already
                        // observable through the debug stats).
                        group_stack.push(GroupFrame {
                            parent: current.clone(),
                            lease: None,
                            tag: command.boundary_index as usize,
                            region,
                        });
                    }
                }
            }
            // A plain INLINE begin draws nothing (the pinned plan's
            // inline arms are no-ops).
        } else if command.command_kind == command_kind::END_FILTER
            || command.command_kind == command_kind::END_EFFECT
        {
            // A group ends: composite the bucket into the parent under
            // the group's parameters, then restore the parent target,
            // viewport and globals. An inline (or demoted) group frame
            // only restores - the pinned plan's inline arms draw
            // nothing.
            //
            // rev 16 (ticket 51): the pop is tagged - the END command's
            // boundary_index is the group's BEGIN record index for a
            // matched pair, so the frame that begin pushed is the one
            // this end restores (an unmatched end's boundary_index is
            // its own marker, which no frame carries). Filter groups
            // keep the exact R15 shape: only their ISOLATED ends act.
            let is_filter = command.command_kind == command_kind::END_FILTER;
            let frame_at_top = !group_stack.is_empty()
                && group_stack
                    .last()
                    .is_some_and(|entry| entry.tag == command.boundary_index as usize);
            if frame_at_top && (command.filter_target == 1 || !is_filter) {
                let entry = group_stack.pop().ok_or(renderer_status::ERR_DEVICE)?;
                let parent = entry.parent.clone();
                // Restore the parent's target, viewport and globals
                // BEFORE compositing: the composite's vertex divides by
                // the globals' viewport size and rasterizes through the
                // PARENT's viewport, so the bucket's globals must not
                // leak into its own draw.
                current = parent.clone();
                active_render_target = current.rtv.clone();
                frame.viewport = current.viewport;
                set_target_globals(&ctx, &frame.globals, current.size)?;
                if let Some(lease) = entry.lease {
                    let source = ActiveTarget {
                        rtv: lease.rtv.clone(),
                        srv: lease.srv.clone(),
                        viewport: D3D11_VIEWPORT {
                            TopLeftX: 0.0,
                            TopLeftY: 0.0,
                            Width: lease.size[0],
                            Height: lease.size[1],
                            MinDepth: 0.0,
                            MaxDepth: 1.0,
                        },
                        origin: [0.0, 0.0],
                        size: lease.size,
                    };
                    if is_filter {
                        let boundary = snapshot
                            .boundaries
                            .get(command.boundary_index as usize)
                            .ok_or(renderer_status::ERR_BAD_VALUE)?;
                        let ops = resolve_filter_op_chain(snapshot, boundary);
                        composite_filter_group(
                            state,
                            draw,
                            slot_index,
                            slot_generation,
                            &frame,
                            &source,
                            &parent,
                            &lease,
                            boundary,
                            ops,
                            window,
                        )?;
                    } else {
                        let effect = snapshot
                            .effects
                            .get(command.boundary_index as usize)
                            .ok_or(renderer_status::ERR_BAD_VALUE)?;
                        composite_effect_group(
                            state,
                            draw,
                            slot_index,
                            slot_generation,
                            &frame,
                            snapshot,
                            atlas,
                            &source,
                            &parent,
                            effect,
                            entry.region,
                            window,
                        )?;
                    }
                    let surface_state = resolve_slot_mut(state, slot_index, slot_generation)?;
                    surface_state.draw_targets.group_pool.release(&lease);
                }
                unsafe {
                    ctx.OMSetRenderTargets(Some(slice::from_ref(&current.rtv)), None);
                }
            }
            // An unmatched end, or an inline group whose begin pushed
            // nothing, draws nothing (the pinned plan's Inline arms are
            // no-ops).
        }
        // Plain inline begins (no offset, no lease) draw nothing (the
        // pinned plan's Inline arms are no-ops).
    }

    // The pin's retain_surface_views: after the command loop, keep
    // only the capture views this scene still references.
    retain_surface_views(draw, &snapshot.surfaces);

    // Present the offscreen scene by blitting it into the swapchain.
    if use_offscreen {
        dx_blit(
            state,
            draw,
            slot_index,
            slot_generation,
            &frame,
            &scene_srv,
            &swapchain_rtv,
        )?;
    }
    unsafe {
        ctx.OMSetRenderTargets(Some(slice::from_ref(&swapchain_rtv)), None);
    }
    // The frame-end pool prune (viz's UpdateRenderPassTextures rule):
    // the backings no group used this frame are dropped; the used ones
    // are retained across frames.
    {
        let surface_state = resolve_slot_mut(state, slot_index, slot_generation)?;
        surface_state.draw_targets.group_pool.prune_unused();
    }
    Ok(())
}

/// The command's index within the snapshot's command stream (the group
/// extent pre-pass keys on it).
fn command_index(snapshot: &Snapshot, command: &GpuiGoSceneCommandRecord) -> usize {
    snapshot
        .commands
        .iter()
        .position(|candidate| std::ptr::eq(candidate, command))
        .unwrap_or(usize::MAX)
}

/// The R16 pre-pass: every group's LOCAL region and DEVICE footprint,
/// keyed by its BEGIN command's index.
///
/// The **region** is what the walk acquires the bucket for: the union
/// of the primitives in the group's span and its nested groups'
/// footprints (a nested filter group contributes its dilated record
/// bounds; a nested effect group its footprint - mapped through its
/// matrix for transform groups). Effect groups that CLIP use their
/// record bounds instead (content outside the rrect is dropped). The
/// **footprint** is what the group's PARENT must cover: the region
/// again for filter and plain effect groups, the mapped region (the
/// AABB of the matrix-mapped corners) for a transform group - the
/// parent's own region must contain where this group's composite
/// lands, and a parent transform group's region is its LOCAL
/// (untransformed) extent, so the mapping applies exactly once, by
/// the group that owns the matrix.
///
/// rev 16 (ticket 51): effect-group BEGINs track regardless of the
/// plan's inline/isolated flag - a translation-only fast-path group
/// still offsets its children inside the parent (the parent must
/// cover the translated footprint), and a promoted inline group
/// (scene_update_property turned its translation into a matrix)
/// acquires its bucket from this same table. Filter groups keep the
/// isolated-only accounting (an unmatched filter marker renders
/// inline - its children union individually).
fn group_content_extents(snapshot: &Snapshot) -> Vec<(usize, [f32; 4], [f32; 4])> {
    #[derive(Clone, Copy)]
    struct Acc {
        min_x: f32,
        min_y: f32,
        max_x: f32,
        max_y: f32,
        empty: bool,
    }
    impl Acc {
        fn union_bounds(&mut self, x: f32, y: f32, w: f32, h: f32) {
            if w <= 0.0 || h <= 0.0 {
                return;
            }
            if self.empty {
                self.min_x = x;
                self.min_y = y;
                self.max_x = x + w;
                self.max_y = y + h;
                self.empty = false;
                return;
            }
            self.min_x = self.min_x.min(x);
            self.min_y = self.min_y.min(y);
            self.max_x = self.max_x.max(x + w);
            self.max_y = self.max_y.max(y + h);
        }
        fn extent(&self, fallback: [f32; 4]) -> [f32; 4] {
            if self.empty {
                fallback
            } else {
                [
                    self.min_x,
                    self.min_y,
                    self.max_x - self.min_x,
                    self.max_y - self.min_y,
                ]
            }
        }
    }
    #[derive(Clone, Copy, PartialEq)]
    enum MarkerKind {
        Filter,
        Effect,
    }
    let empty_acc = || Acc {
        min_x: 0.0,
        min_y: 0.0,
        max_x: 0.0,
        max_y: 0.0,
        empty: true,
    };
    let record_bounds_of = |bits: &crate::scene::GpuiGoSceneBounds| {
        [
            f32::from_bits(bits.x),
            f32::from_bits(bits.y),
            f32::from_bits(bits.w),
            f32::from_bits(bits.h),
        ]
    };
    let mut result: Vec<(usize, [f32; 4], [f32; 4])> = Vec::new();
    let mut stack: Vec<(MarkerKind, usize, Acc)> = Vec::new();
    for (index, command) in snapshot.commands.iter().enumerate() {
        match command.command_kind {
            command_kind::BATCH => {
                if let Some(extent) = batch_bounds(snapshot, command) {
                    if let Some((_, _, acc)) = stack.last_mut() {
                        acc.union_bounds(extent[0], extent[1], extent[2], extent[3]);
                    }
                }
            }
            command_kind::BEGIN_FILTER => {
                if command.filter_target == 1 {
                    stack.push((MarkerKind::Filter, index, empty_acc()));
                }
            }
            command_kind::BEGIN_EFFECT => {
                stack.push((MarkerKind::Effect, index, empty_acc()));
            }
            command_kind::END_FILTER | command_kind::END_EFFECT => {
                let is_filter = command.command_kind == command_kind::END_FILTER;
                let wants_pop = stack.last().is_some_and(|(kind, _, _)| {
                    *kind == if is_filter { MarkerKind::Filter } else { MarkerKind::Effect }
                }) && (command.filter_target == 1 || !is_filter);
                if !wants_pop {
                    continue;
                }
                let Some((_, begin_index, acc)) = stack.pop() else {
                    continue;
                };
                let (region, footprint) = if is_filter {
                    let record_bounds = snapshot
                        .boundaries
                        .get(command.boundary_index as usize)
                        .map(|record| {
                            let dilation = GAUSSIAN_CUTOFF_STANDARD_DEVIATIONS
                                * record.max_blur_radius();
                            let base = record_bounds_of(&record.bounds);
                            [
                                base[0] - dilation,
                                base[1] - dilation,
                                base[2] + 2.0 * dilation,
                                base[3] + 2.0 * dilation,
                            ]
                        })
                        .unwrap_or([0.0, 0.0, 0.0, 0.0]);
                    (record_bounds, record_bounds)
                } else {
                    match snapshot.effects.get(command.boundary_index as usize) {
                        None => {
                            let extent = acc.extent([0.0, 0.0, 0.0, 0.0]);
                            (extent, extent)
                        }
                        Some(record) => {
                            let clips = (record.flags
                                & (crate::scene::effect_flags::CLIP_TO_BOUNDS
                                    | crate::scene::effect_flags::ROUNDED_CLIP_ONLY))
                                != 0;
                            let base = record_bounds_of(&record.bounds);
                            let region = if clips {
                                base
                            } else {
                                acc.extent(base)
                            };
                            // The parent-frame footprint: mapped through
                            // the group's own matrix when it carries a
                            // transform (the identity-mapped region for
                            // the plain translation, which
                            // map_bounds_corners computes exactly as the
                            // translated footprint).
                            let footprint =
                                if (record.flags & crate::scene::effect_flags::HAS_TRANSFORM) != 0 {
                                    crate::scene::map_bounds_corners(region, crate::scene::effect_matrix(record))
                                } else {
                                    region
                                };
                            (region, footprint)
                        }
                    }
                };
                result.push((begin_index, region, footprint));
                if let Some((_, _, parent)) = stack.last_mut() {
                    parent.union_bounds(footprint[0], footprint[1], footprint[2], footprint[3]);
                }
            }
            _ => {}
        }
    }
    result
}

/// One batch command's primitive bounds union (the extent pre-pass).
fn batch_bounds(snapshot: &Snapshot, command: &GpuiGoSceneCommandRecord) -> Option<[f32; 4]> {
    let range = command.range_start as usize..command.range_end as usize;
    let mut min_x = f32::MAX;
    let mut min_y = f32::MAX;
    let mut max_x = f32::MIN;
    let mut max_y = f32::MIN;
    let mut seen = false;
    let mut union = |x: f32, y: f32, w: f32, h: f32| {
        if w <= 0.0 || h <= 0.0 {
            return;
        }
        seen = true;
        min_x = min_x.min(x);
        min_y = min_y.min(y);
        max_x = max_x.max(x + w);
        max_y = max_y.max(y + h);
    };
    match command.primitive_kind {
        batch_primitive_kind::SHADOWS => {
            for record in &snapshot.shadows[range] {
                union(
                    f32::from_bits(record.bounds.x),
                    f32::from_bits(record.bounds.y),
                    f32::from_bits(record.bounds.w),
                    f32::from_bits(record.bounds.h),
                );
            }
        }
        batch_primitive_kind::QUADS => {
            for record in &snapshot.quads[range] {
                union(
                    f32::from_bits(record.bounds.x),
                    f32::from_bits(record.bounds.y),
                    f32::from_bits(record.bounds.w),
                    f32::from_bits(record.bounds.h),
                );
            }
        }
        batch_primitive_kind::UNDERLINES => {
            for record in &snapshot.underlines[range] {
                union(
                    record.bounds.origin.x.0,
                    record.bounds.origin.y.0,
                    record.bounds.size.width.0,
                    record.bounds.size.height.0,
                );
            }
        }
        batch_primitive_kind::MONOCHROME_SPRITES => {
            for record in &snapshot.monochrome_sprites[range] {
                union(
                    record.bounds.origin.x.0,
                    record.bounds.origin.y.0,
                    record.bounds.size.width.0,
                    record.bounds.size.height.0,
                );
            }
        }
        batch_primitive_kind::SUBPIXEL_SPRITES => {
            for record in &snapshot.subpixel_sprites[range] {
                union(
                    record.bounds.origin.x.0,
                    record.bounds.origin.y.0,
                    record.bounds.size.width.0,
                    record.bounds.size.height.0,
                );
            }
        }
        batch_primitive_kind::POLYCHROME_SPRITES => {
            for record in &snapshot.polychrome_sprites[range] {
                union(
                    record.bounds.origin.x.0,
                    record.bounds.origin.y.0,
                    record.bounds.size.width.0,
                    record.bounds.size.height.0,
                );
            }
        }
        batch_primitive_kind::SURFACES => {
            for record in &snapshot.surfaces[range] {
                union(
                    f32::from_bits(record.bounds.x),
                    f32::from_bits(record.bounds.y),
                    f32::from_bits(record.bounds.w),
                    f32::from_bits(record.bounds.h),
                );
            }
        }
        batch_primitive_kind::BACKDROP_FILTERS => {
            for record in &snapshot.backdrops[range] {
                union(
                    f32::from_bits(record.bounds.x),
                    f32::from_bits(record.bounds.y),
                    f32::from_bits(record.bounds.w),
                    f32::from_bits(record.bounds.h),
                );
            }
        }
        batch_primitive_kind::PATHS => {
            for record in &snapshot.paths[range] {
                union(
                    record.clipped_bounds.origin.x.0,
                    record.clipped_bounds.origin.y.0,
                    record.clipped_bounds.size.width.0,
                    record.clipped_bounds.size.height.0,
                );
            }
        }
        _ => return None,
    }
    if !seen {
        return None;
    }
    Some([min_x, min_y, max_x - min_x, max_y - min_y])
}

/// The pin's `upload_scene_buffers`: upload every non-empty primitive
/// array once into its pipeline's whole-frame instance buffer.
fn upload_scene_buffers(
    device: &ID3D11Device,
    ctx: &ID3D11DeviceContext,
    draw: &mut DrawGlobal,
    snapshot: &Snapshot,
) -> Result<(), i32> {
    let pipelines = &mut draw.pipelines;
    macro_rules! upload {
        ($pipeline:ident, $data:expr) => {
            if !$data.is_empty() {
                pipelines
                    .$pipeline
                    .update(device, ctx, $data.len())
                    .map_err(|_| renderer_status::ERR_CAPACITY)?;
                unsafe {
                    let mut dest = std::mem::zeroed();
                    ctx.Map(
                        &pipelines.$pipeline.buffer,
                        0,
                        D3D11_MAP_WRITE_DISCARD,
                        0,
                        Some(&mut dest),
                    )
                    .map_err(|e| e.code().0)?;
                    std::ptr::copy_nonoverlapping(
                        $data.as_ptr() as *const u8,
                        dest.pData as *mut u8,
                        std::mem::size_of_val($data.as_slice()),
                    );
                    ctx.Unmap(&pipelines.$pipeline.buffer, 0);
                }
            }
        };
    }
    upload!(shadow_pipeline, snapshot.shadows);
    upload!(quad_pipeline, snapshot.quads);
    upload!(underline_pipeline, snapshot.underlines);
    upload!(mono_sprites, snapshot.monochrome_sprites);
    upload!(subpixel_sprites, snapshot.subpixel_sprites);
    upload!(poly_sprites, snapshot.polychrome_sprites);
    // rev 16 (ticket 52): the scene's gradient table - one 64-byte
    // instance per gradient (the ABI record reshaped for the HLSL
    // structured-buffer layout) - and the baked ramp rows into the
    // DYNAMIC ramp texture (grown in place when the row count rises;
    // the unused tail rows are cleared).
    if !snapshot.gradients.is_empty() {
        if pipelines.gradient_table_capacity < snapshot.gradients.len() {
            let capacity = snapshot
                .gradients
                .len()
                .checked_next_power_of_two()
                .ok_or(renderer_status::ERR_CAPACITY)?;
            let (buffer, view) =
                create_instance_buffer(device, GRADIENT_INSTANCE_STRIDE, capacity)?;
            pipelines.gradient_table_buffer = buffer;
            pipelines.gradient_table_view = view;
            pipelines.gradient_table_capacity = capacity;
        }
        let mut instances: Vec<[u32; GRADIENT_INSTANCE_STRIDE / 4]> =
            vec![[0u32; GRADIENT_INSTANCE_STRIDE / 4]; snapshot.gradients.len()];
        for (instance, gradient) in instances.iter_mut().zip(snapshot.gradients.iter()) {
            instance[0] = gradient.kind;
            instance[1] = gradient.tile;
            instance[2] = gradient.color_space;
            instance[3] = gradient.stop_count;
            instance[4] = gradient.geometry[0];
            instance[5] = gradient.geometry[1];
            instance[6] = gradient.geometry[2];
            instance[7] = gradient.geometry[3];
            instance[8] = gradient.geometry[4];
            instance[9] = gradient.geometry[5];
            instance[10] = gradient.geometry[6];
            instance[11] = gradient.geometry[7];
            instance[12] = gradient.stop_row;
            instance[13] = gradient.record_size;
        }
        unsafe {
            let mut dest = std::mem::zeroed();
            ctx.Map(
                &pipelines.gradient_table_buffer,
                0,
                D3D11_MAP_WRITE_DISCARD,
                0,
                Some(&mut dest),
            )
            .map_err(|e| e.code().0)?;
            let bytes = std::mem::size_of_val(instances.as_slice());
            debug_assert_eq!(bytes, instances.len() * GRADIENT_INSTANCE_STRIDE);
            std::ptr::copy_nonoverlapping(
                instances.as_ptr() as *const u8,
                dest.pData as *mut u8,
                bytes,
            );
            ctx.Unmap(&pipelines.gradient_table_buffer, 0);
        }
    }
    let rows = snapshot.gradients.len().max(1) as u32;
    if pipelines.ramp_rows < rows {
        let (texture, srv) = create_gradient_ramp_texture(device, rows)?;
        pipelines.ramp_texture = Some(texture);
        pipelines.ramp_srv = srv;
        pipelines.ramp_rows = rows;
    }
    let ramp_texture = pipelines
        .ramp_texture
        .clone()
        .ok_or(renderer_status::ERR_DEVICE)?;
    unsafe {
        let mut mapped = std::mem::zeroed();
        ctx.Map(&ramp_texture, 0, D3D11_MAP_WRITE_DISCARD, 0, Some(&mut mapped))
            .map_err(|e| e.code().0)?;
        let row_bytes = crate::scene::GRADIENT_RAMP_WIDTH as usize * 4;
        let usable = snapshot.gradient_ramps.len();
        for row in 0..pipelines.ramp_rows as usize {
            let dest_row = (mapped.pData as *mut u8).add(row * mapped.RowPitch as usize);
            if row * row_bytes < usable {
                let source = &snapshot.gradient_ramps
                    [row * row_bytes..(row + 1) * row_bytes];
                std::ptr::copy_nonoverlapping(source.as_ptr(), dest_row, row_bytes);
            } else {
                std::ptr::write_bytes(dest_row, 0, row_bytes);
            }
        }
        ctx.Unmap(&ramp_texture, 0);
    }
    Ok(())
}

/// Binds the scene's gradient table (t7), ramp texture (t8) and clamp
/// sampler (s9) on the PIXEL stage (rev 16, ticket 52) for the
/// quad/shadow draws - the gradient-table backgrounds sample them per
/// pixel. Rebound on EVERY quad/shadow batch: the sprite draws bind
/// their atlas texture over the neighboring slots, so the state cannot
/// be assumed to survive (the same reason every batch rebinds its own
/// textures).
fn bind_gradient_resources(ctx: &ID3D11DeviceContext, draw: &DrawGlobal) {
    let table = [draw.pipelines.gradient_table_view.clone()];
    let ramp = [draw.pipelines.ramp_srv.clone()];
    let sampler = [draw.globals.clamp_sampler.clone()];
    unsafe {
        ctx.PSSetShaderResources(GRADIENT_TABLE_REGISTER, Some(&table));
        ctx.PSSetShaderResources(GRADIENT_RAMP_REGISTER, Some(&ramp));
        ctx.PSSetSamplers(GRADIENT_SAMPLER_REGISTER, Some(&sampler));
    }
}

/// Resolves a sprite batch's atlas texture view through the atlas
/// service (the pin's `atlas.get_texture_view`), mapping the plan's
/// texture kind tags to the atlas pools.
fn sprite_texture(
    atlas: u64,
    kind: u32,
    texture_index: u32,
) -> Result<[Option<ID3D11ShaderResourceView>; 1], i32> {
    let view = crate::atlas::texture_srv(atlas, kind, texture_index)?;
    Ok([Some(view)])
}

/// Converts a render-plan batch range into draw arguments (the pin's
/// `instance_range`), refusing ranges D3D11 cannot address.
fn instance_range(start: u32, end: u32) -> Result<InstanceRange, i32> {
    InstanceRange::new(start as usize..end as usize).ok_or(renderer_status::ERR_CAPACITY)
}

fn resolve_slot(
    state: &RendererState,
    slot_index: usize,
    slot_generation: u32,
) -> Result<&SurfaceState, i32> {
    super::resolve_slot(state, slot_index, slot_generation)
}

fn ensure_path_resources(
    state: &mut RendererState,
    slot_index: usize,
    slot_generation: u32,
    device: &ID3D11Device,
) -> Result<(), i32> {
    let surface_state = resolve_slot_mut(state, slot_index, slot_generation)?;
    if surface_state.draw_targets.path.is_none() {
        let width = surface_state.width;
        let height = surface_state.height;
        surface_state.draw_targets.path = Some(PathResources::new(device, width, height)?);
    }
    Ok(())
}

/// Creates the per-surface coverage rasterization resources (rev 16,
/// ticket 52) when the scene carries clip-path coverage entries.
fn ensure_coverage_resources(
    state: &mut RendererState,
    slot_index: usize,
    slot_generation: u32,
    device: &ID3D11Device,
) -> Result<(), i32> {
    let surface_state = resolve_slot_mut(state, slot_index, slot_generation)?;
    if surface_state.draw_targets.coverage.is_none() {
        let width = surface_state.width;
        let height = surface_state.height;
        surface_state.draw_targets.coverage =
            Some(CoverageResources::new(device, width, height)?);
    }
    Ok(())
}

/// Creates the per-surface blend backdrop-copy target (rev 16, ticket
/// 52) when the scene carries blend groups.
fn ensure_blend_backdrop(
    state: &mut RendererState,
    slot_index: usize,
    slot_generation: u32,
    device: &ID3D11Device,
) -> Result<(), i32> {
    let surface_state = resolve_slot_mut(state, slot_index, slot_generation)?;
    if surface_state.draw_targets.blend_backdrop.is_none() {
        let width = surface_state.width;
        let height = surface_state.height;
        surface_state.draw_targets.blend_backdrop =
            Some(BlendBackdropTarget::new(device, width, height)?);
    }
    Ok(())
}

/// The clip-path COVERAGE rasterization (rev 16, ticket 52): rasterizes
/// every coverage entry's referenced path into the per-surface coverage
/// target, in the BASE frame's coordinates (the window; the rasterized
/// fill is solid white, so the resolved texture's channels carry the
/// path's coverage). An out-of-range path index rasterizes nothing (the
/// mask reads empty - the element is hidden, the design §3.5 rule).
#[allow(clippy::too_many_arguments)]
fn rasterize_coverage(
    state: &mut RendererState,
    draw: &mut DrawGlobal,
    slot_index: usize,
    slot_generation: u32,
    frame: &FrameBindings<'_>,
    snapshot: &Snapshot,
) -> Result<(), i32> {
    let surface_state = resolve_slot_mut(state, slot_index, slot_generation)?;
    let coverage = surface_state
        .draw_targets
        .coverage
        .as_ref()
        .ok_or(renderer_status::ERR_DEVICE)?;
    let msaa_view = coverage.msaa_view.clone().ok_or(renderer_status::ERR_DEVICE)?;
    let coverage_texture = coverage.texture.clone().ok_or(renderer_status::ERR_DEVICE)?;
    let msaa_texture = coverage.msaa_texture.clone().ok_or(renderer_status::ERR_DEVICE)?;
    let ctx = frame.device_context;
    unsafe {
        ctx.ClearRenderTargetView(&msaa_view, &[0.0; 4]);
        let bound = Some(msaa_view.clone());
        ctx.OMSetRenderTargets(Some(slice::from_ref(&bound)), None);
    }
    // The rasterization instances: each coverage entry's path vertices
    // with the SOLID WHITE fill (the silhouette is the coverage).
    let mut vertices: Vec<PathRasterizationVertex> = Vec::new();
    for entry in snapshot.coverage.iter() {
        let Some(path) = snapshot.paths.get(entry.path_index as usize) else {
            continue;
        };
        let fill_hsla: gpui::Hsla = gpui::Hsla::new(0.0, 0.0, 1.0, 1.0);
        let fill: gpui::Background = gpui::Background::from(fill_hsla);
        for vertex in path.vertices.iter() {
            vertices.push(PathRasterizationVertex {
                xy_position: vertex.xy,
                curve_position: vertex.st,
                color: fill.clone(),
                bounds: path.clipped_bounds,
            });
        }
    }
    if !vertices.is_empty() {
        let device = state
            .device
            .as_ref()
            .ok_or(renderer_status::ERR_DEVICE)?
            .device
            .clone();
        let pipeline = &mut draw.pipelines.path_rasterization_pipeline;
        pipeline.update(&device, ctx, vertices.len())?;
        unsafe {
            let mut dest = std::mem::zeroed();
            ctx.Map(&pipeline.buffer, 0, D3D11_MAP_WRITE_DISCARD, 0, Some(&mut dest))
                .map_err(|e| e.code().0)?;
            std::ptr::copy_nonoverlapping(
                vertices.as_ptr() as *const u8,
                dest.pData as *mut u8,
                std::mem::size_of_val(vertices.as_slice()),
            );
            ctx.Unmap(&pipeline.buffer, 0);
        }
        let vertex_count =
            u32::try_from(vertices.len()).map_err(|_| renderer_status::ERR_CAPACITY)?;
        pipeline.draw_vertices(frame, vertex_count)?;
    }
    // Resolve MSAA into the coverage texture (the pin's path resolve).
    unsafe {
        ctx.ResolveSubresource(
            &coverage_texture,
            0,
            &msaa_texture,
            0,
            DXGI_FORMAT_B8G8R8A8_UNORM,
        );
    }
    Ok(())
}

/// Copies the parent target's content into the blend backdrop target
/// (rev 16, ticket 52): the 1:1 shader-pass copy through the vendored
/// backdrop-crop pass with a zero margin and the whole parent as the
/// region (the parent's SRV sampled at its own layout; the copy's
/// viewport is the PARENT's so the content lands at the parent's
/// coordinates in the window-sized copy target).
fn copy_blend_backdrop(
    draw: &DrawGlobal,
    frame: &FrameBindings<'_>,
    parent: &ActiveTarget,
    parent_srv: &Option<ID3D11ShaderResourceView>,
    backdrop_rtv: &Option<ID3D11RenderTargetView>,
) -> Result<(), i32> {
    let params = BlurUniforms::copy(parent.size);
    let crop = BackdropCropUniforms {
        origin: [0.0, 0.0],
        source_size: parent.size,
        region_size: parent.size,
        margin: [0.0, 0.0],
    };
    dx_blur_pass(
        draw,
        frame,
        &draw.pipelines.backdrop_crop_vertex,
        &draw.pipelines.backdrop_crop_fragment,
        &draw.pipelines.blur_blend_replace,
        backdrop_rtv,
        parent_srv,
        params,
        &parent.viewport,
        D3D_PRIMITIVE_TOPOLOGY_TRIANGLELIST,
        3,
        false,
        BlurPassExtras::Crop(&crop),
    )
}

fn ensure_blur_resources(
    state: &mut RendererState,
    slot_index: usize,
    slot_generation: u32,
    device: &ID3D11Device,
) -> Result<(), i32> {
    let surface_state = resolve_slot_mut(state, slot_index, slot_generation)?;
    if surface_state.draw_targets.blur.is_none() {
        let width = surface_state.width;
        let height = surface_state.height;
        surface_state.draw_targets.blur = Some(BlurResources::new(device, width, height)?);
    }
    Ok(())
}

/// The pin's `draw_paths_to_intermediate`: build the rasterization
/// vertices, draw them into the MSAA target, resolve into the
/// intermediate and restore the active render target.
#[allow(clippy::too_many_arguments)]
fn draw_paths_to_intermediate(
    state: &mut RendererState,
    draw: &mut DrawGlobal,
    slot_index: usize,
    slot_generation: u32,
    frame: &FrameBindings<'_>,
    paths: &[crate::scene::SnapshotPath],
    rasterization_vertex_count: usize,
    path_rasterization_vertices: &mut Vec<PathRasterizationVertex>,
    active_render_target: &Option<ID3D11RenderTargetView>,
    swapchain_rtv: &Option<ID3D11RenderTargetView>,
) -> Result<(), i32> {
    if paths.is_empty() {
        return Ok(());
    }

    path_rasterization_vertices.clear();
    path_rasterization_vertices.reserve(rasterization_vertex_count);
    for path in paths {
        for vertex in &path.vertices {
            path_rasterization_vertices.push(PathRasterizationVertex {
                xy_position: vertex.xy,
                curve_position: vertex.st,
                color: path.color,
                bounds: path.clipped_bounds,
            });
        }
    }
    debug_assert_eq!(
        path_rasterization_vertices.len(),
        rasterization_vertex_count
    );

    let surface_state = resolve_slot_mut(state, slot_index, slot_generation)?;
    let path = surface_state
        .draw_targets
        .path
        .as_ref()
        .ok_or(renderer_status::ERR_DEVICE)?;
    let msaa_view = path
        .msaa_view
        .clone()
        .ok_or(renderer_status::ERR_DEVICE)?;
    let path_texture = path.texture.clone();
    let msaa_texture = path.msaa_texture.clone();
    // Clear intermediate MSAA texture and set it as the render target.
    unsafe {
        frame.device_context.ClearRenderTargetView(&msaa_view, &[0.0; 4]);
        let bound = Some(msaa_view.clone());
        frame
            .device_context
            .OMSetRenderTargets(Some(slice::from_ref(&bound)), None);
    }

    // Upload + draw the rasterization vertices (the pin's per-batch
    // buffer update).
    {
        let device = state
            .device
            .as_ref()
            .ok_or(renderer_status::ERR_DEVICE)?
            .device
            .clone();
        let pipeline = &mut draw.pipelines.path_rasterization_pipeline;
        pipeline.update(&device, frame.device_context, path_rasterization_vertices.len())?;
        unsafe {
            let mut dest = std::mem::zeroed();
            frame
                .device_context
                .Map(&pipeline.buffer, 0, D3D11_MAP_WRITE_DISCARD, 0, Some(&mut dest))
                .map_err(|e| e.code().0)?;
            std::ptr::copy_nonoverlapping(
                path_rasterization_vertices.as_ptr() as *const u8,
                dest.pData as *mut u8,
                std::mem::size_of_val(path_rasterization_vertices.as_slice()),
            );
            frame.device_context.Unmap(&pipeline.buffer, 0);
        }
        let vertex_count =
            u32::try_from(rasterization_vertex_count).map_err(|_| renderer_status::ERR_CAPACITY)?;
        pipeline.draw_vertices(frame, vertex_count)?;
    }

    // Resolve MSAA to the non-MSAA intermediate texture.
    unsafe {
        frame.device_context.ResolveSubresource(
            &path_texture,
            0,
            &msaa_texture,
            0,
            DXGI_FORMAT_B8G8R8A8_UNORM,
        );
        // Restore the active render target (the offscreen scene/group
        // target when blurring, otherwise the swapchain) so the path
        // sprites land on the correct surface.
        let restore_target = if active_render_target.is_some() {
            active_render_target.clone()
        } else {
            swapchain_rtv.clone()
        };
        if let Some(rtv) = restore_target.as_ref() {
            let bound = Some(rtv.clone());
            frame
                .device_context
                .OMSetRenderTargets(Some(slice::from_ref(&bound)), None);
        }
    }
    Ok(())
}

/// The pin's `draw_paths_from_intermediate`: copy each path's bounds
/// from the intermediate to the drawable — per path when they share the
/// first's order (disjoint bounds), else one minimal spanning rect.
#[allow(clippy::too_many_arguments)]
fn draw_paths_from_intermediate(
    state: &mut RendererState,
    draw: &mut DrawGlobal,
    slot_index: usize,
    slot_generation: u32,
    frame: &FrameBindings<'_>,
    paths: &[crate::scene::SnapshotPath],
    sprite_count: usize,
    path_sprites: &mut Vec<PathSprite>,
) -> Result<(), i32> {
    let Some(first_path) = paths.first() else {
        return Ok(());
    };

    // When copying paths from the intermediate texture to the drawable,
    // each pixel must only be copied once, in case of transparent paths.
    path_sprites.clear();
    path_sprites.reserve(sprite_count);
    if paths.last().is_some_and(|path| path.order == first_path.order) {
        path_sprites.extend(paths.iter().map(|path| PathSprite {
            bounds: path.clipped_bounds,
        }));
    } else {
        let mut bounds = first_path.clipped_bounds;
        for path in paths.iter().skip(1) {
            bounds = bounds.union(&path.clipped_bounds);
        }
        path_sprites.push(PathSprite { bounds });
    }
    debug_assert_eq!(path_sprites.len(), sprite_count);

    let surface_state = resolve_slot_mut(state, slot_index, slot_generation)?;
    let srv = surface_state
        .draw_targets
        .path
        .as_ref()
        .and_then(|path| path.srv.clone())
        .ok_or(renderer_status::ERR_DEVICE)?;

    {
        let device = state
            .device
            .as_ref()
            .ok_or(renderer_status::ERR_DEVICE)?
            .device
            .clone();
        let pipeline = &mut draw.pipelines.path_sprite_pipeline;
        pipeline.update(&device, frame.device_context, path_sprites.len())?;
        unsafe {
            let mut dest = std::mem::zeroed();
            frame
                .device_context
                .Map(&pipeline.buffer, 0, D3D11_MAP_WRITE_DISCARD, 0, Some(&mut dest))
                .map_err(|e| e.code().0)?;
            std::ptr::copy_nonoverlapping(
                path_sprites.as_ptr() as *const u8,
                dest.pData as *mut u8,
                std::mem::size_of_val(path_sprites.as_slice()),
            );
            frame.device_context.Unmap(&pipeline.buffer, 0);
        }
        let instances =
            InstanceRange::from_start(sprite_count).ok_or(renderer_status::ERR_CAPACITY)?;
        let texture = [Some(srv)];
        pipeline.draw_instances(frame, Some(&texture), instances.first(), instances.count())?;
    }
    Ok(())
}

/// The pin's `draw_surfaces` (directx_renderer.rs 1174-1248): one draw
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
    let sampler = [draw.globals.sampler.clone()];

    for (index, surface) in surfaces.iter().enumerate() {
        if surface.source_tag != surface_source_kind::CAPTURE_FRAME {
            // The pin's Unsupported import bail (the whole draw fails —
            // an explicit error, never a silent blank surface).
            return Err(renderer_status::ERR_UNSUPPORTED_SOURCE);
        }
        // The 64-bit capture frame handle is packed across the two
        // reserved words (low 32 in [0], high 32 in [1] — the scene
        // record's payload is u32 words).
        let frame_handle = (surface.source_reserved[0] as u64)
            | ((surface.source_reserved[1] as u64) << 32);
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
        let mask = &surface.content_mask;
        let uniforms = SurfaceUniforms {
            bounds: ShaderBounds {
                origin: vec2f(f32::from_bits(bounds.x), f32::from_bits(bounds.y)),
                size: vec2f(f32::from_bits(bounds.w), f32::from_bits(bounds.h)),
            },
            content_mask: ShaderContentMask {
                bounds: ShaderBounds {
                    origin: vec2f(
                        f32::from_bits(mask.bounds.x),
                        f32::from_bits(mask.bounds.y),
                    ),
                    size: vec2f(
                        f32::from_bits(mask.bounds.w),
                        f32::from_bits(mask.bounds.h),
                    ),
                },
                // The scene ABI carries hard masks (no fade edges), so
                // the shader mask's fade is zero — the same value the
                // ABI's other masked primitives use.
                // rev 16 (ticket 50): the extended mask's per-edge
                // fades ride the pinned ContentMask (the pinned
                // surface shader evaluates them); the radii apply only
                // in the vendored pipelines this revision (the recorded
                // gap handed to ticket 52).
                fade_out: ShaderEdges {
                    top: f32::from_bits(mask.fade_out[1]),
                    right: f32::from_bits(mask.fade_out[2]),
                    bottom: f32::from_bits(mask.fade_out[3]),
                    left: f32::from_bits(mask.fade_out[0]),
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

        // The surface pipeline binds its full state RAW (a two-plane
        // texture pair at the primary slot included): the tracker's
        // cache cannot survive it (§3.12).
        frame.binds.invalidate();
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
    let active_keys: std::collections::HashSet<u64> = surfaces
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
}

/// Run a single blur pass (the pin's `dx_blur_pass`): a fullscreen (or
/// composite) draw sampling `source_srv` into `target_rtv` with `params`
/// in the blur constant buffer (DATA_REGISTER).
#[allow(clippy::too_many_arguments)]
fn dx_blur_pass(
    draw: &DrawGlobal,
    frame: &FrameBindings<'_>,
    vertex: &ID3D11VertexShader,
    fragment: &ID3D11PixelShader,
    blend: &ID3D11BlendState,
    target_rtv: &Option<ID3D11RenderTargetView>,
    source_srv: &Option<ID3D11ShaderResourceView>,
    params: BlurUniforms,
    viewport: &D3D11_VIEWPORT,
    topology: D3D_PRIMITIVE_TOPOLOGY,
    vertex_count: u32,
    clear: bool,
    extras: BlurPassExtras<'_>,
) -> Result<(), i32> {
    let ctx = frame.device_context;
    update_buffer(ctx, &draw.pipelines.blur_params_buffer, &[params])?;
    let binds = frame.binds.clone();
    match &extras {
        BlurPassExtras::None => {}
        BlurPassExtras::Composite(uniforms) => {
            // rev 15.1 (browser-core ticket 77, the ticket-41 fix):
            // `match &extras` binds `uniforms: &&CompositeExtensionsUniforms`,
            // so the element type of the literal must be `**uniforms` —
            // `&[*uniforms]` is a one-element slice of REFERENCES (8 bytes),
            // and update_buffer copies data.len() * size_of::<&T>() into the
            // 112-byte cbuffer (the shader then reads a zero `enabled` and
            // every colour function was a GPU no-op in rev 15).
            update_buffer(ctx, &draw.pipelines.composite_extensions_buffer, &[**uniforms])?;
        }
        BlurPassExtras::Crop(uniforms) => {
            // The same dereference shape for the 32-byte backdrop-crop
            // cbuffer (the rev-15 backdrop blur read a zeroed fold).
            update_buffer(ctx, &draw.pipelines.backdrop_crop_buffer, &[**uniforms])?;
        }
    }
    unsafe {
        // Unbind any SRV at the blur slot; the target must not be bound
        // as input.
        binds.bind_primary_srv_pixel(ctx, None);
        if clear {
            let rtv = target_rtv.as_ref().ok_or(renderer_status::ERR_DEVICE)?;
            ctx.ClearRenderTargetView(rtv, &[0.0; 4]);
        }
        ctx.OMSetRenderTargets(Some(slice::from_ref(target_rtv)), None);
        binds.bind_viewport(ctx, viewport);
        binds.bind_topology(ctx, topology);
        binds.bind_vertex_shader(ctx, vertex);
        binds.bind_pixel_shader(ctx, fragment);
        binds.bind_cbuffer_pair(ctx, &frame.globals);
        binds.bind_cbuffer_data(ctx, DATA_REGISTER, &draw.pipelines.blur_params_buffer, BindStages::Both);
        match &extras {
            BlurPassExtras::None => {}
            BlurPassExtras::Composite(_) => {
                // The vertex stage reads the extensions too (rev 16,
                // ticket 51: the transformed composite's matrix lives
                // there).
                binds.bind_cbuffer_extensions(ctx, &draw.pipelines.composite_extensions_buffer);
            }
            BlurPassExtras::Crop(_) => {
                binds.bind_cbuffer_crop(ctx, &draw.pipelines.backdrop_crop_buffer);
            }
        }
        if let Some(sampler) = frame.globals.sampler.as_ref() {
            binds.bind_primary_sampler(ctx, sampler);
        }
        binds.bind_primary_srv_pixel(ctx, source_srv.as_ref());
        binds.bind_blend_state(ctx, blend);
        ctx.DrawInstanced(vertex_count, 1, 0, 0);
        // Unbind the source so the target can be rebound as a render
        // target next.
        binds.bind_primary_srv_pixel(ctx, None);
    }
    Ok(())
}

/// Blur `source_srv` through the half-res ping/pong textures and
/// composite the result into `target_rtv` (the pin's
/// `dx_blur_and_composite`, shared by the backdrop and content-filter
/// paths; rev 15 grew the colour-matrix payload and the backdrop's
/// mirror crop).
///
/// rev 15 (browser-core ticket 39):
/// * The filter record's composed colour matrix applies in the composite
///   pass (after any blur — a per-pixel linear map commutes with the
///   uniform-per-channel gaussian, so the CSS chain order is preserved);
///   a chain with no matrix filters composites exactly as the pin did.
/// * A backdrop (`clip_rounded`) first crops the source to the element's
///   pixel-covered bounds with mirrored margins (CSS filter-effects-2
///   clips the backdrop to the border box; Chromium mirrors the edge
///   pixels — viz skia_renderer.cc:3254, SkTileMode::kMirror), so
///   content just outside the element no longer bleeds into the blur.
///   The blurred input is then the crop texture and the composite
///   samples it region-relative.
/// The drop-shadow op's σ: the CSS blur radius halved (Chromium
/// `ShadowData::BlurRadiusToStdDev`, "per spec, sigma is exactly half
/// the blur radius" - shadow_data.h:76-88; the record carries the CSS
/// radius, the renderer derives σ, mirroring the Go paint seam's
/// PaintBoxShadow).
fn drop_shadow_sigma(op: &crate::scene::GpuiGoSceneFilterOpRecord) -> f32 {
    f32::from_bits(op.blur_radius) * 0.5
}

/// Draws one DROP-SHADOW op (rev 16, ticket 52, the design's §3.9): the
/// group's blurred ALPHA silhouette, tinted with the op's color,
/// offset by (dx, dy) and composited PREMULTIPLIED beneath the content
/// quad. The op's own blur chain runs when its radius is nonzero (the
/// half-res downsample + the separable gaussian, exactly the content
/// chain's shape; a zero radius samples the raw bucket). The shadow
/// quad is the content composite's dilated bounds dilated by the op's
/// 3σ and translated by the op's offset; `sample_offset` carries the
/// offset (the translation composite's rule).
#[allow(clippy::too_many_arguments)]
fn draw_drop_shadow_op(
    draw: &DrawGlobal,
    frame: &FrameBindings<'_>,
    blur_source: &Option<ID3D11ShaderResourceView>,
    target: &ActiveTarget,
    ping_rtv: &Option<ID3D11RenderTargetView>,
    ping_srv: &Option<ID3D11ShaderResourceView>,
    pong_rtv: &Option<ID3D11RenderTargetView>,
    pong_srv: &Option<ID3D11ShaderResourceView>,
    source_size: [f32; 2],
    blur_size: [f32; 2],
    bounds: gpui::Bounds<gpui::ScaledPixels>,
    content_mask: gpui::Bounds<gpui::ScaledPixels>,
    opacity: f32,
    content_blur_radius: f32,
    op: &crate::scene::GpuiGoSceneFilterOpRecord,
) -> Result<(), i32> {
    let sigma = drop_shadow_sigma(op);
    let (dx, dy) = (f32::from_bits(op.offset[0]), f32::from_bits(op.offset[1]));
    // The shadow's source: the op's own blurred silhouette or the raw
    // bucket (a zero-radius op).
    let shadow_source: Option<ID3D11ShaderResourceView>;
    let shadow_source_size: [f32; 2];
    if let Some(kernel) = BlurKernel::for_radius(sigma) {
        let half_vp = D3D11_VIEWPORT {
            TopLeftX: 0.0,
            TopLeftY: 0.0,
            Width: blur_size[0],
            Height: blur_size[1],
            MinDepth: 0.0,
            MaxDepth: 1.0,
        };
        dx_blur_pass(
            draw,
            frame,
            &draw.pipelines.blur_downsample_vertex,
            &draw.pipelines.blur_downsample_fragment,
            &draw.pipelines.blur_blend_replace,
            ping_rtv,
            blur_source,
            BlurUniforms::downsample(source_size, blur_size),
            &half_vp,
            D3D_PRIMITIVE_TOPOLOGY_TRIANGLELIST,
            3,
            true,
            BlurPassExtras::None,
        )?;
        dx_blur_pass(
            draw,
            frame,
            &draw.pipelines.blur_vertex,
            &draw.pipelines.blur_fragment,
            &draw.pipelines.blur_blend_replace,
            pong_rtv,
            ping_srv,
            BlurUniforms::gaussian(BlurAxis::Horizontal, blur_size, kernel),
            &half_vp,
            D3D_PRIMITIVE_TOPOLOGY_TRIANGLELIST,
            3,
            true,
            BlurPassExtras::None,
        )?;
        dx_blur_pass(
            draw,
            frame,
            &draw.pipelines.blur_vertex,
            &draw.pipelines.blur_fragment,
            &draw.pipelines.blur_blend_replace,
            ping_rtv,
            pong_srv,
            BlurUniforms::gaussian(BlurAxis::Vertical, blur_size, kernel),
            &half_vp,
            D3D_PRIMITIVE_TOPOLOGY_TRIANGLELIST,
            3,
            true,
            BlurPassExtras::None,
        )?;
        shadow_source = ping_srv.clone();
        shadow_source_size = blur_size;
    } else {
        shadow_source = blur_source.clone();
        shadow_source_size = source_size;
    }
    // The shadow quad: the content composite's dilated bounds (the
    // record bounds dilated by the CONTENT blur's 3σ - the blurred
    // silhouette spreads exactly as far as the blurred content),
    // dilated by the op's own 3σ and translated by the op's offset.
    let content_dilation =
        GAUSSIAN_CUTOFF_STANDARD_DEVIATIONS * content_blur_radius;
    let shadow_dilation = GAUSSIAN_CUTOFF_STANDARD_DEVIATIONS * sigma;
    let shadow_bounds = bounds
        .dilate(gpui::ScaledPixels(content_dilation))
        .dilate(gpui::ScaledPixels(shadow_dilation));
    let shadow_bounds = gpui::Bounds {
        origin: gpui::point(
            gpui::ScaledPixels(shadow_bounds.origin.x.0 + dx),
            gpui::ScaledPixels(shadow_bounds.origin.y.0 + dy),
        ),
        size: shadow_bounds.size,
    };
    let uniforms = BlurUniforms::composite(
        shadow_bounds,
        content_mask,
        snapshot_radii(&[0, 0, 0, 0]),
        0.0,
        opacity,
        FilterCompositeClip::ContentShape,
        shadow_source_size,
        source_size,
    );
    // The tint: the op's straight sRGBA color (the HSLA record
    // converted through the pinned conversion).
    let tint = gpui::hsla_to_rgba(gpui::Hsla::new(
        f32::from_bits(op.color.h) * 360.0,
        f32::from_bits(op.color.s),
        f32::from_bits(op.color.l),
        f32::from_bits(op.color.a),
    ));
    let mut extensions = CompositeExtensionsUniforms::default();
    extensions.tint_color = [tint.red, tint.green, tint.blue, tint.alpha];
    extensions.sample_offset = [dx, dy];
    if std::env::var("GPUI_DBG_SHADOW").is_ok() {
        eprintln!(
            "drop-shadow: sigma={} off=({},{}) shadow_bounds=({},{},{},{}) shadow_source_size={:?} source_size={:?} tint={:?}",
            sigma, dx, dy,
            shadow_bounds.origin.x.0, shadow_bounds.origin.y.0, shadow_bounds.size.width.0, shadow_bounds.size.height.0,
            shadow_source_size, source_size, extensions.tint_color
        );
    }
    dx_blur_pass(
        draw,
        frame,
        &draw.pipelines.drop_shadow_composite_vertex,
        &draw.pipelines.drop_shadow_composite_fragment,
        &draw.pipelines.blur_blend_composite,
        &target.rtv,
        &shadow_source,
        uniforms,
        &target.viewport,
        D3D_PRIMITIVE_TOPOLOGY_TRIANGLESTRIP,
        4,
        false,
        BlurPassExtras::Composite(&extensions),
    )?;
    Ok(())
}


/// 50: `dx_blur_and_composite` generalized off the full-window targets -
/// the group's pooled bucket is the blur source, the parent target the
/// destination, the bucket's own half-resolution scratch (or the
/// window ping/pong when the bucket IS window-sized, preserving R15's
/// exact scratch) runs the chain, and the composite samples the bucket
/// region-relative ((window position - bucket origin) / bucket size).
/// The blur, colour-matrix and clip semantics are R15's, unchanged.
///
/// rev 16, ticket 52: the DROP-SHADOW ops (the appended op chain)
/// render BENEATH the content - for each op, the bucket's blurred
/// ALPHA silhouette (the gaussian averages each channel, so the
/// blurred alpha IS the blurred silhouette; the chain runs at the op's
/// own σ = radius/2, CSS's shadow rule, Chromium `shadow_data.h:76-88`
/// BlurAsSigma - a zero-radius op samples the raw bucket), tinted with
/// the op's color and offset by (dx, dy), drawn premultiplied over the
/// parent; then the content composite runs unchanged. The R15 early-
/// out (no blur, no matrices, nothing to draw) fires only WITHOUT ops.
#[allow(clippy::too_many_arguments)]
fn composite_filter_group(
    state: &mut RendererState,
    draw: &DrawGlobal,
    slot_index: usize,
    slot_generation: u32,
    frame: &FrameBindings<'_>,
    source: &ActiveTarget,
    target: &ActiveTarget,
    lease: &GroupLease,
    rec: &crate::scene::GpuiGoSceneFilterRecord,
    ops: &[crate::scene::GpuiGoSceneFilterOpRecord],
    window: (u32, u32),
) -> Result<(), i32> {
    let bounds = snapshot_bounds(&rec.bounds);
    let content_mask = snapshot_bounds(&rec.content_mask.bounds);
    let corner_radii = snapshot_radii(&rec.corner_radii);
    let corner_smoothing = f32::from_bits(rec.corner_smoothing);
    let blur_radius = rec.max_blur_radius();
    let opacity = f32::from_bits(rec.opacity);
    let blur_source = source.srv.clone();
    let source_size = source.size;
    let blur_size = [
        downsampled_dimension(lease.size[0] as u32) as f32,
        downsampled_dimension(lease.size[1] as u32) as f32,
    ];
    // The blur chain's scratch: the window pair when the bucket is the
    // whole window (R15's exact textures and sizes), the bucket's own
    // pair otherwise.
    let window_pair = {
        let surface_state = resolve_slot(state, slot_index, slot_generation)?;
        match surface_state.draw_targets.blur.as_ref() {
            Some(blur) => Some((
                blur.ping_rtv.clone(),
                blur.ping_srv.clone(),
                blur.pong_rtv.clone(),
                blur.pong_srv.clone(),
            )),
            None => None,
        }
    };
    let bucket_is_window = lease.size[0] as u32 == window.0 && lease.size[1] as u32 == window.1;
    // rev 16, ticket 52: a drop-shadow op with a nonzero radius needs the
    // half-res scratch even when the content chain carries no blur of
    // its own (the shadow's own chain runs through the same pair).
    let needs_blur_chain = BlurKernel::for_radius(blur_radius).is_some()
        || ops
            .iter()
            .any(|op| BlurKernel::for_radius(drop_shadow_sigma(op)).is_some());
    let (ping_rtv, ping_srv, pong_rtv, pong_srv) = if !needs_blur_chain {
        // A matrix-only chain composites the bucket directly - the
        // half-resolution scratch is never touched.
        (None, None, None, None)
    } else if bucket_is_window {
        match window_pair {
            Some(pair) => pair,
            None => return Err(renderer_status::ERR_DEVICE),
        }
    } else {
        if lease.half_a_rtv.is_none() {
            return Err(renderer_status::ERR_DEVICE);
        }
        (
            lease.half_a_rtv.clone(),
            lease.half_a_srv.clone(),
            lease.half_b_rtv.clone(),
            lease.half_b_srv.clone(),
        )
    };

    // The composite extensions: the chain's per-function colour
    // matrices (rev 15.1 semantics, unchanged) and the bucket-relative
    // sampling offset.
    let mut extensions = CompositeExtensionsUniforms::default();
    let matrix_count = rec.color_matrix_count.min(COLOR_MATRIX_CAPACITY);
    if matrix_count > 0 {
        for entry in 0..matrix_count as usize {
            let base = entry * 20;
            let mut row = |index: usize| -> [f32; 4] {
                [
                    f32::from_bits(rec.color_matrix[base + index]),
                    f32::from_bits(rec.color_matrix[base + index + 1]),
                    f32::from_bits(rec.color_matrix[base + index + 2]),
                    f32::from_bits(rec.color_matrix[base + index + 3]),
                ]
            };
            extensions.entries[entry].row0 = row(0);
            extensions.entries[entry].row1 = row(5);
            extensions.entries[entry].row2 = row(10);
            extensions.entries[entry].row3 = row(15);
            extensions.entries[entry].offset = [
                f32::from_bits(rec.color_matrix[base + 4]),
                f32::from_bits(rec.color_matrix[base + 9]),
                f32::from_bits(rec.color_matrix[base + 14]),
                f32::from_bits(rec.color_matrix[base + 19]),
            ];
        }
        extensions.count = matrix_count;
    }
    // The bucket sits at the frame's origin (ticket 51): the sampling
    // offset is zero and the frame's coordinates are the composite's
    // coordinates.
    extensions.sample_offset = [0.0, 0.0];

    let half_vp = D3D11_VIEWPORT {
        TopLeftX: 0.0,
        TopLeftY: 0.0,
        Width: blur_size[0],
        Height: blur_size[1],
        MinDepth: 0.0,
        MaxDepth: 1.0,
    };

    // Downsample source -> ping, then separable gaussian ping -> pong ->
    // ping. A matrix-only chain (no blur kernel) composites the bucket
    // directly - and a zero-radius, zero-matrix chain with NO drop-shadow
    // ops keeps the pinned early-out (nothing to draw).
    let composited_srv: Option<ID3D11ShaderResourceView>;
    let kernel = BlurKernel::for_radius(blur_radius);
    // rev 16, ticket 52: the drop-shadow ops render BENEATH the content -
    // the shadow's own blur chain (at the op's σ) runs before the
    // content's chain (both reuse the same scratch ping/pong; the shadow
    // reads its result immediately).
    for op in ops {
        draw_drop_shadow_op(
            draw,
            frame,
            &blur_source,
            target,
            &ping_rtv,
            &ping_srv,
            &pong_rtv,
            &pong_srv,
            source_size,
            blur_size,
            bounds,
            content_mask,
            opacity,
            blur_radius,
            op,
        )?;
    }
    if let Some(kernel) = kernel {
        dx_blur_pass(
            draw,
            frame,
            &draw.pipelines.blur_downsample_vertex,
            &draw.pipelines.blur_downsample_fragment,
            &draw.pipelines.blur_blend_replace,
            &ping_rtv,
            &blur_source,
            BlurUniforms::downsample(source_size, blur_size),
            &half_vp,
            D3D_PRIMITIVE_TOPOLOGY_TRIANGLELIST,
            3,
            true,
            BlurPassExtras::None,
        )?;
        dx_blur_pass(
            draw,
            frame,
            &draw.pipelines.blur_vertex,
            &draw.pipelines.blur_fragment,
            &draw.pipelines.blur_blend_replace,
            &pong_rtv,
            &ping_srv,
            BlurUniforms::gaussian(BlurAxis::Horizontal, blur_size, kernel),
            &half_vp,
            D3D_PRIMITIVE_TOPOLOGY_TRIANGLELIST,
            3,
            true,
            BlurPassExtras::None,
        )?;
        dx_blur_pass(
            draw,
            frame,
            &draw.pipelines.blur_vertex,
            &draw.pipelines.blur_fragment,
            &draw.pipelines.blur_blend_replace,
            &ping_rtv,
            &pong_srv,
            BlurUniforms::gaussian(BlurAxis::Vertical, blur_size, kernel),
            &half_vp,
            D3D_PRIMITIVE_TOPOLOGY_TRIANGLELIST,
            3,
            true,
            BlurPassExtras::None,
        )?;
        composited_srv = ping_srv;
    } else if rec.color_matrix_count > 0 || !ops.is_empty() {
        composited_srv = blur_source;
    } else {
        // The pinned early-out (R15 semantics preserved for filter
        // boundaries): a zero-radius blur chain with no matrix and no
        // drop-shadow ops has nothing to draw.
        return Ok(());
    }

    // Content blur bleeds ~3*radius past the box; composite over a
    // dilated rect (content filters do not clip - R15 semantics).
    let composite_bounds = bounds.dilate(gpui::ScaledPixels(
        GAUSSIAN_CUTOFF_STANDARD_DEVIATIONS * blur_radius,
    ));
    let composite_uniforms = BlurUniforms::composite(
        composite_bounds,
        content_mask,
        corner_radii,
        corner_smoothing,
        opacity,
        FilterCompositeClip::ContentShape,
        blur_size,
        source_size,
    );
    let (composite_vertex, composite_fragment) = if composite_uniforms.corner_smoothing > 0.0 {
        (
            &draw.pipelines.smoothed_blur_composite_vertex,
            &draw.pipelines.smoothed_blur_composite_fragment,
        )
    } else {
        (
            &draw.pipelines.blur_composite_vertex,
            &draw.pipelines.blur_composite_fragment,
        )
    };
    dx_blur_pass(
        draw,
        frame,
        composite_vertex,
        composite_fragment,
        &draw.pipelines.blur_blend_composite,
        &target.rtv,
        &composited_srv,
        composite_uniforms,
        &target.viewport,
        D3D_PRIMITIVE_TOPOLOGY_TRIANGLESTRIP,
        4,
        false,
        BlurPassExtras::Composite(&extensions),
    )?;
    Ok(())
}

/// The R16 group composite for EFFECT groups (rev 16, ticket 50; the
/// matrix composite is ticket 51): the explicit NO-BLUR composite - one
/// textured quad over the group's bounds under the group's opacity,
/// rounded clip (when the group clips: `clip_to_bounds` /
/// `rounded_clip_only`) and rectangular content mask, sampled from the
/// pooled bucket region-relative. This is the group-opacity path (the
/// single-primitive case never reaches it: the plan folds it into a
/// per-primitive multiply).
///
/// rev 16, ticket 51: a group carrying a 2x3 transform composites
/// through the TRANSFORMED path - the bucket rect's corners mapped
/// through the matrix (`transformed_blur_composite`), the local-space
/// rounded clip and content mask evaluated at the interpolated LOCAL
/// position, the bucket sampled at the quad's unit coordinates with
/// linear filtering. A translation-only group (the 2x2 is the
/// identity) takes the axis-aligned path with its rects translated -
/// it keeps the smoothed-corner composite 50 built.
///
/// rev 16, ticket 52: the MASK composite (the `has_mask` flag - the
/// b5 mask fields plus the bound mask source texture; the fragments
/// multiply the sampled group colour by the mask's alpha or
/// luminance, dst_in on the premultiplied source; a masked group's
/// quad is its BOUNDS, the mask rect, per Chromium's "masked content
/// is clipped to the mask's bounding box") and the BLEND composite
/// (the `has_blend` flag - the parent's content is copied into the
/// backdrop target first, then one `blend_composite` pass applies the
/// W3C blend formula against that copy, writing with no fixed
/// blending; the matrix path and the axis-aligned path share it, the
/// identity matrix for the latter).
/// The effect record's BOUNDS as a window-space rect (the mask rect /
/// the clipped composite quad).
fn effect_quad(effect: &crate::scene::GpuiGoSceneEffectRecord) -> [f32; 4] {
    [
        f32::from_bits(effect.bounds.x),
        f32::from_bits(effect.bounds.y),
        f32::from_bits(effect.bounds.w),
        f32::from_bits(effect.bounds.h),
    ]
}

/// Unbinds the composite's mask (t10) and backdrop (t11) texture slots
/// (rev 16, ticket 52): the mask sources and the backdrop copy are
/// read-only during the composites, but the slots must not leak into
/// later passes that bind their own textures there (the same reason
/// `dx_blur_pass` unbinds its primary source after every pass).
fn unbind_mask_and_backdrop(ctx: &ID3D11DeviceContext) {
    let null_srv: [Option<ID3D11ShaderResourceView>; 1] = [None];
    unsafe {
        ctx.PSSetShaderResources(MASK_TEXTURE_REGISTER, Some(&null_srv));
        ctx.PSSetShaderResources(BACKDROP_TEXTURE_REGISTER, Some(&null_srv));
    }
}

/// Resolves one effect group's MASK source (rev 16, ticket 52) and
/// binds it at the composite's mask slot (t10): the gradient mask
/// samples the shared ramp (t7/t8/t9 - rebound here for the composite's
/// frame), the atlas mask resolves the polychrome texture view through
/// the atlas service, and the coverage mask resolves the per-surface
/// coverage target's SRV. Returns the EFFECTIVE kind (the record's
/// kind, or 4 when the source is unresolvable - the composite draws
/// NOTHING, the design §3.5's "an invalid mask hides the element" -
/// never a failed frame) and the mask texture to bind (None for the
/// gradient and invalid sources).
#[allow(clippy::too_many_arguments)]
fn resolve_mask_source(
    state: &mut RendererState,
    ctx: &ID3D11DeviceContext,
    draw: &DrawGlobal,
    snapshot: &Snapshot,
    atlas: u64,
    effect: &crate::scene::GpuiGoSceneEffectRecord,
    slot_index: usize,
    slot_generation: u32,
) -> Result<(u32, Option<ID3D11ShaderResourceView>), i32> {
    if (effect.flags & crate::scene::effect_flags::HAS_MASK) == 0 {
        return Ok((0, None));
    }
    match effect.mask_kind {
        1 => {
            // The gradient mask: rebind the shared gradient table +
            // ramp (the composite's frame may follow sprite draws that
            // bound their atlas textures over the slots); the reference
            // must name a table entry.
            bind_gradient_resources(ctx, draw);
            if (effect.mask_reference as usize) < snapshot.gradients.len() {
                Ok((1, None))
            } else {
                Ok((4, None))
            }
        }
        2 => {
            // The atlas mask: the polychrome texture the reference
            // names (the whole texture stretched over the mask rect -
            // the mask-image contract).
            match crate::atlas::texture_srv(atlas, 1, effect.mask_reference) {
                Ok(view) => Ok((2, Some(view))),
                Err(_) => Ok((4, None)),
            }
        }
        3 => {
            // The coverage mask: the per-surface coverage target's SRV,
            // valid when the entry AND its path resolve.
            let surface_state = resolve_slot(state, slot_index, slot_generation)?;
            let srv = surface_state
                .draw_targets
                .coverage
                .as_ref()
                .and_then(|coverage| coverage.srv.clone());
            let valid = snapshot
                .coverage
                .get(effect.mask_reference as usize)
                .and_then(|entry| snapshot.paths.get(entry.path_index as usize))
                .is_some();
            if valid {
                Ok((3, srv))
            } else {
                Ok((4, None))
            }
        }
        _ => Ok((4, None)),
    }
}

/// The per-surface blend backdrop-copy target's SRV (rev 16, ticket 52;
/// bound at t11 for the blend composite after the copy pass).
fn surface_state_after_copy(
    state: &mut RendererState,
    slot_index: usize,
    slot_generation: u32,
) -> Result<Option<ID3D11ShaderResourceView>, i32> {
    let surface_state = resolve_slot(state, slot_index, slot_generation)?;
    Ok(surface_state
        .draw_targets
        .blend_backdrop
        .as_ref()
        .and_then(|backdrop| backdrop.srv.clone()))
}

/// Resolves a filter boundary's DROP-SHADOW op chain (rev 16, ticket
/// 52): the referenced scene-level chain sliced to the record's op
/// count; an out-of-range index or a zero count is the R15 legacy
/// semantics (an EMPTY chain - nothing changes for every existing
/// record).
fn resolve_filter_op_chain<'a>(
    snapshot: &'a Snapshot,
    boundary: &crate::scene::GpuiGoSceneFilterRecord,
) -> &'a [crate::scene::GpuiGoSceneFilterOpRecord] {
    if boundary.op_chain_count == 0 {
        return &[];
    }
    match snapshot
        .filter_op_chains
        .get(boundary.op_chain_index as usize)
    {
        Some(chain) => chain.as_slice()
            .get(..boundary.op_chain_count as usize)
            .unwrap_or(&[]),
        None => &[],
    }
}

#[allow(clippy::too_many_arguments)]
fn composite_effect_group(
    state: &mut RendererState,
    draw: &DrawGlobal,
    slot_index: usize,
    slot_generation: u32,
    frame: &FrameBindings<'_>,
    snapshot: &Snapshot,
    atlas: u64,
    source: &ActiveTarget,
    target: &ActiveTarget,
    effect: &crate::scene::GpuiGoSceneEffectRecord,
    region: [f32; 4],
    window: (u32, u32),
) -> Result<(), i32> {
    let clips = (effect.flags
        & (crate::scene::effect_flags::CLIP_TO_BOUNDS
            | crate::scene::effect_flags::ROUNDED_CLIP_ONLY))
        != 0;
    let has_mask = (effect.flags & crate::scene::effect_flags::HAS_MASK) != 0;
    let has_blend = (effect.flags & crate::scene::effect_flags::HAS_BLEND) != 0;
    let opacity = f32::from_bits(effect.opacity);
    let mut extensions = CompositeExtensionsUniforms::default();
    // The mask resolution (ticket 52): the b5 mask fields plus the
    // mask source texture at t10 (the atlas or coverage source; the
    // gradient mask samples the shared ramp directly). An
    // unresolvable source is kind 4 - the composite draws NOTHING (the
    // design §3.5's "an invalid mask hides the element").
    let (mask_kind, mask_texture) = resolve_mask_source(
        state,
        frame.device_context,
        draw,
        snapshot,
        atlas,
        effect,
        slot_index,
        slot_generation,
    )?;
    {
        let bound = [mask_texture];
        unsafe {
            frame
                .device_context
                .PSSetShaderResources(MASK_TEXTURE_REGISTER, Some(&bound));
        }
    }
    extensions.mask_kind = mask_kind;
    extensions.mask_reference = effect.mask_reference;
    extensions.mask_mode = effect.mask_mode;
    extensions.blend_mode = effect.blend_mode;
    // The matrix composite (ticket 51): the subtree rendered into its
    // bucket in the group's LOCAL frame (the children's local
    // coordinates, bucket pixel p = local p), so the composite maps the
    // bucket rect through the record's row-major 2x3 (the
    // transform-origin and the group's parent-frame position already
    // resolved by the caller into the matrix). The clip and the mask
    // stay in the local frame - CSS clips inside the element, then the
    // transform applies - and the fragment samples the bucket at the
    // quad's unit coordinates (linear filtering).
    let matrix_composite = (effect.flags & crate::scene::effect_flags::HAS_TRANSFORM) != 0
        && !crate::scene::effect_translation_only(effect);
    if has_blend {
        // The blend composite (ticket 52): copy the parent's content
        // into the backdrop target, then one pass blending the source
        // against that copy. The pass covers the group's composite quad
        // (the BOUNDS when the group clips or masks - the mask rect;
        // the content region otherwise), mapped through the matrix for
        // transform groups (the identity otherwise), and the b5
        // `backdrop_size` carries the parent's logical size.
        let surface_state = resolve_slot(state, slot_index, slot_generation)?;
        let backdrop_rtv = surface_state
            .draw_targets
            .blend_backdrop
            .as_ref()
            .and_then(|backdrop| backdrop.rtv.clone());
        let parent_srv = target.srv.clone();
        match (backdrop_rtv, parent_srv) {
            (Some(backdrop_rtv), Some(parent_srv)) => {
                copy_blend_backdrop(draw, frame, target, &Some(parent_srv), &Some(backdrop_rtv))?;
                // Unbind the backdrop RTV from the output-merger stage
                // BEFORE binding its SRV: D3D11 forbids one resource as
                // input and output simultaneously (the copy pass left
                // the backdrop bound as the render target, and the
                // runtime would silently unbind the conflicting SRV -
                // the blend fragment would sample nothing). The
                // composite pass sets its own target right after.
                unsafe {
                    frame.device_context.OMSetRenderTargets(None, None);
                }
                // The blend fragment samples the backdrop COPY at the
                // device position - bind its SRV at t11.
                let backdrop_srv = surface_state_after_copy(
                    state,
                    slot_index,
                    slot_generation,
                )?;
                let bound_backdrop = [backdrop_srv];
                unsafe {
                    frame
                        .device_context
                        .PSSetShaderResources(BACKDROP_TEXTURE_REGISTER, Some(&bound_backdrop));
                }
                let clip = if clips || has_mask {
                    FilterCompositeClip::RoundedBounds
                } else {
                    FilterCompositeClip::ContentShape
                };
                // The composite quad: the group's BOUNDS (the mask rect)
                // when the group clips or masks, the content region
                // otherwise; translated for an axis-aligned group, in
                // the LOCAL frame for a matrix composite.
                let quad = if clips || has_mask {
                    effect_quad(effect)
                } else {
                    region
                };
                if matrix_composite {
                    let [m00, m01, m10, m11, tx, ty] = crate::scene::effect_matrix(effect);
                    extensions.transform_row0 = [m00, m01, tx, 0.0];
                    extensions.transform_row1 = [m10, m11, ty, 0.0];
                    let uniforms = BlurUniforms::composite(
                        quad_bounds(quad),
                        quad_bounds(effect_mask_bounds(effect)),
                        snapshot_radii(&effect.corner_radii),
                        f32::from_bits(effect.corner_smoothing),
                        opacity,
                        clip,
                        source.size,
                        source.size,
                    );
                    extensions.backdrop_size = target.size;
                    dx_blur_pass(
                        draw,
                        frame,
                        &draw.pipelines.blend_composite_vertex,
                        &draw.pipelines.blend_composite_fragment,
                        &draw.pipelines.blur_blend_replace,
                        &target.rtv,
                        &source.srv,
                        uniforms,
                        &target.viewport,
                        D3D_PRIMITIVE_TOPOLOGY_TRIANGLESTRIP,
                        4,
                        false,
                        BlurPassExtras::Composite(&extensions),
                    )?;
                } else {
                    // The axis-aligned blend group: the identity matrix
                    // (the blend composite's vertex is the transformed
                    // shape) with the translation folded into the quad
                    // (the translation composite's rule).
                    let (tx, ty) = if (effect.flags
                        & crate::scene::effect_flags::HAS_TRANSFORM)
                        != 0
                    {
                        crate::scene::effect_translation(effect)
                    } else {
                        (0.0, 0.0)
                    };
                    extensions.transform_row0 = [1.0, 0.0, tx, 0.0];
                    extensions.transform_row1 = [0.0, 1.0, ty, 0.0];
                    extensions.sample_offset = [tx, ty];
                    let translated = [
                        quad[0] + tx,
                        quad[1] + ty,
                        quad[2],
                        quad[3],
                    ];
                    let uniforms = BlurUniforms::composite(
                        quad_bounds(translated),
                        quad_bounds(effect_mask_bounds(effect)),
                        snapshot_radii(&effect.corner_radii),
                        f32::from_bits(effect.corner_smoothing),
                        opacity,
                        clip,
                        source.size,
                        source.size,
                    );
                    extensions.backdrop_size = target.size;
                    dx_blur_pass(
                        draw,
                        frame,
                        &draw.pipelines.blend_composite_vertex,
                        &draw.pipelines.blend_composite_fragment,
                        &draw.pipelines.blur_blend_replace,
                        &target.rtv,
                        &source.srv,
                        uniforms,
                        &target.viewport,
                        D3D_PRIMITIVE_TOPOLOGY_TRIANGLESTRIP,
                        4,
                        false,
                        BlurPassExtras::Composite(&extensions),
                    )?;
                }
                // Unbind the mask/backdrop textures so later passes can
                // freely reuse their slots (the existing code unbinds the
                // primary source; the mask and backdrop sources get the
                // same treatment).
                unbind_mask_and_backdrop(frame.device_context);
                return Ok(());
            }
            _ => {
                // The backdrop cannot be sampled (no offscreen scene
                // texture / no backdrop target - a defensive path that
                // the plan's `uses_offscreen_target` flag prevents):
                // degrade to the source-over composite below, never a
                // failed frame.
            }
        }
    }
    if matrix_composite {
        // The quad rect: the group's LOCAL content region (the bucket's
        // un-quantized extent) - or the record BOUNDS when the group
        // clips or masks (the mask rect; Chromium clips masked content
        // to the mask's bounding box). The quantization margin between
        // the region and the bucket never reaches the target, and the
        // wrap-addressed sampling edge stays at the content's own
        // edge. The group's clip (when it clips) is the rrect coverage
        // over the record's content mask - not the quad.
        let clip = if clips || has_mask {
            FilterCompositeClip::RoundedBounds
        } else {
            FilterCompositeClip::ContentShape
        };
        let quad = if clips || has_mask {
            effect_quad(effect)
        } else {
            region
        };
        let uniforms = BlurUniforms::composite(
            quad_bounds(quad),
            quad_bounds(effect_mask_bounds(effect)),
            snapshot_radii(&effect.corner_radii),
            f32::from_bits(effect.corner_smoothing),
            opacity,
            clip,
            source.size,
            source.size,
        );
        // The row-major 2x3 rows: the ABI record's transform layout
        // (device = row.xy . p + row.z).
        let [m00, m01, m10, m11, tx, ty] = crate::scene::effect_matrix(effect);
        extensions.transform_row0 = [m00, m01, tx, 0.0];
        extensions.transform_row1 = [m10, m11, ty, 0.0];
        dx_blur_pass(
            draw,
            frame,
            &draw.pipelines.transformed_blur_composite_vertex,
            &draw.pipelines.transformed_blur_composite_fragment,
            &draw.pipelines.blur_blend_composite,
            &target.rtv,
            &source.srv,
            uniforms,
            &target.viewport,
            D3D_PRIMITIVE_TOPOLOGY_TRIANGLESTRIP,
            4,
            false,
            BlurPassExtras::Composite(&extensions),
        )?;
        unbind_mask_and_backdrop(frame.device_context);
        return Ok(());
    }
    // The translation a translation-only group carries (ticket 51):
    // every rect the axis-aligned composite draws shifts by it, so the
    // bucket's content lands at its translated position (the bucket
    // itself rendered the children at their local positions).
    let (tx, ty) = if (effect.flags & crate::scene::effect_flags::HAS_TRANSFORM) != 0 {
        crate::scene::effect_translation(effect)
    } else {
        (0.0, 0.0)
    };
    let translate = |bits: &crate::scene::GpuiGoSceneBounds| -> [f32; 4] {
        [
            f32::from_bits(bits.x) + tx,
            f32::from_bits(bits.y) + ty,
            f32::from_bits(bits.w),
            f32::from_bits(bits.h),
        ]
    };
    // rev 16, ticket 52: a masked group's composite quad is its
    // BOUNDS (the mask rect), like a clipping group's.
    let (composite_bounds, corner_radii, corner_smoothing, clip) = if clips || has_mask {
        // The composite quad is the group's (rounded) bounds; content
        // outside the rrect is dropped - CSS overflow clipping.
        (
            quad_bounds(translate(&effect.bounds)),
            snapshot_radii(&effect.corner_radii),
            f32::from_bits(effect.corner_smoothing),
            FilterCompositeClip::RoundedBounds,
        )
    } else {
        // No clip: the composite quad is the content region at its
        // translated position; nothing is lost.
        (
            quad_bounds([region[0] + tx, region[1] + ty, region[2], region[3]]),
            snapshot_radii(&[0, 0, 0, 0]),
            0.0,
            FilterCompositeClip::ContentShape,
        )
    };
    let content_mask = quad_bounds(translate(&effect.content_mask));

    let composite_uniforms = BlurUniforms::composite(
        composite_bounds,
        content_mask,
        corner_radii,
        corner_smoothing,
        opacity,
        clip,
        source.size,
        source.size,
    );
    let (composite_vertex, composite_fragment) = if composite_uniforms.corner_smoothing > 0.0 {
        (
            &draw.pipelines.smoothed_blur_composite_vertex,
            &draw.pipelines.smoothed_blur_composite_fragment,
        )
    } else {
        (
            &draw.pipelines.blur_composite_vertex,
            &draw.pipelines.blur_composite_fragment,
        )
    };
    // The sampling origin is the TRANSLATION only: the bucket's texels
    // ARE the frame's coordinates (the origin-0 scheme), so a pixel at
    // the frame position p samples the bucket at p / bucket size, and
    // the translated quad's pixel p + t samples it at (p + t - t).
    extensions.sample_offset = [tx, ty];
    dx_blur_pass(
        draw,
        frame,
        composite_vertex,
        composite_fragment,
        &draw.pipelines.blur_blend_composite,
        &target.rtv,
        &source.srv,
        composite_uniforms,
        &target.viewport,
        D3D_PRIMITIVE_TOPOLOGY_TRIANGLESTRIP,
        4,
        false,
        BlurPassExtras::Composite(&extensions),
    )?;
    unbind_mask_and_backdrop(frame.device_context);
    let _ = window;
    Ok(())
}

/// One window-space rect as the pinned `Bounds<ScaledPixels>` (the
/// composite uniforms' quad shape; rev 16, ticket 51).
fn quad_bounds(rect: [f32; 4]) -> gpui::Bounds<gpui::ScaledPixels> {
    gpui::Bounds {
        origin: gpui::point(gpui::ScaledPixels(rect[0]), gpui::ScaledPixels(rect[1])),
        size: gpui::Size {
            width: gpui::ScaledPixels(rect[2]),
            height: gpui::ScaledPixels(rect[3]),
        },
    }
}

/// The effect record's own rectangular content mask as a window-space
/// rect.
fn effect_mask_bounds(effect: &crate::scene::GpuiGoSceneEffectRecord) -> [f32; 4] {
    [
        f32::from_bits(effect.content_mask.x),
        f32::from_bits(effect.content_mask.y),
        f32::from_bits(effect.content_mask.w),
        f32::from_bits(effect.content_mask.h),
    ]
}

#[allow(clippy::too_many_arguments)]
fn dx_blur_and_composite(
    state: &mut RendererState,
    draw: &DrawGlobal,
    slot_index: usize,
    slot_generation: u32,
    frame: &FrameBindings<'_>,
    // The CURRENT target: the backdrop's blur source AND its composite
    // destination (rev 16: an ActiveTarget so a backdrop inside an open
    // group samples and draws the group's bucket with the right
    // origin/viewport).
    current: &ActiveTarget,
    rec: &crate::scene::GpuiGoSceneFilterRecord,
    // Backdrop clips to the rounded rect and crops its input; the
    // content-filter group path lives in composite_filter_group.
    clip_rounded: bool,
) -> Result<(), i32> {
    let bounds = snapshot_bounds(&rec.bounds);
    let content_mask = snapshot_bounds(&rec.content_mask.bounds);
    let corner_radii = snapshot_radii(&rec.corner_radii);
    let corner_smoothing = f32::from_bits(rec.corner_smoothing);
    let blur_radius = rec.max_blur_radius();
    let opacity = f32::from_bits(rec.opacity);

    let source_srv = current.srv.clone();
    let target_rtv = current.rtv.clone();
    let full_vp = current.viewport;
    let clip = if clip_rounded {
        FilterCompositeClip::RoundedBounds
    } else {
        FilterCompositeClip::ContentShape
    };

    // The composite extensions: the chain's per-function colour
    // matrices (rev 15.1 — each applied in chain order with the [0,1]
    // clamp between, matching Chrome's 8-bit per-function rasterization;
    // the composed single matrix of rev 15 diverged by a 15.47 mean
    // channel diff on the filters-chain fixture) and the sampling
    // offset (region-relative for the backdrop's crop, (0, 0) — the
    // pinned formula — otherwise).
    let mut extensions = CompositeExtensionsUniforms::default();
    let matrix_count = rec.color_matrix_count.min(COLOR_MATRIX_CAPACITY);
    if matrix_count > 0 {
        for entry in 0..matrix_count as usize {
            let base = entry * 20;
            let mut row = |index: usize| -> [f32; 4] {
                [
                    f32::from_bits(rec.color_matrix[base + index]),
                    f32::from_bits(rec.color_matrix[base + index + 1]),
                    f32::from_bits(rec.color_matrix[base + index + 2]),
                    f32::from_bits(rec.color_matrix[base + index + 3]),
                ]
            };
            extensions.entries[entry].row0 = row(0);
            extensions.entries[entry].row1 = row(5);
            extensions.entries[entry].row2 = row(10);
            extensions.entries[entry].row3 = row(15);
            extensions.entries[entry].offset = [
                f32::from_bits(rec.color_matrix[base + 4]),
                f32::from_bits(rec.color_matrix[base + 9]),
                f32::from_bits(rec.color_matrix[base + 14]),
                f32::from_bits(rec.color_matrix[base + 19]),
            ];
        }
        extensions.count = matrix_count;
    }

    // The blurred input and the normalization the composite samples
    // against: the full target (the pinned behavior) or the backdrop
    // crop. rev 16 (ticket 50, folding in ticket 77's fix): the crop's
    // blur chain runs on the CROP-SIZED half-resolution pair (the
    // scratch must match the blur source, or the gaussian's normalized
    // coordinates zoom a partial-region backdrop's input); the crop's
    // origin is carried SOURCE-relative (window coordinates minus the
    // current target's origin) so a backdrop inside an open group
    // samples the group's bucket correctly.
    let mut blur_source = source_srv.clone();
    let mut source_size = current.size;
    let mut blur_size = [
        downsampled_dimension(current.size[0] as u32) as f32,
        downsampled_dimension(current.size[1] as u32) as f32,
    ];
    let mut ping_rtv: Option<ID3D11RenderTargetView> = None;
    let mut ping_srv: Option<ID3D11ShaderResourceView> = None;
    let mut pong_rtv: Option<ID3D11RenderTargetView> = None;
    let mut pong_srv: Option<ID3D11ShaderResourceView> = None;
    let kernel = BlurKernel::for_radius(blur_radius);
    if clip_rounded {
        let crop = ensure_backdrop_crop(
            state,
            slot_index,
            slot_generation,
            bounds,
            blur_radius,
            current.size,
        )?;
        let crop_uniforms = BackdropCropUniforms {
            origin: [
                crop.origin[0] - current.origin[0],
                crop.origin[1] - current.origin[1],
            ],
            source_size: current.size,
            region_size: crop.region,
            margin: crop.margin,
        };
        // Crop the backdrop into the scratch texture (the region plus
        // its mirrored margins), through the vendored pass.
        dx_blur_pass(
            draw,
            frame,
            &draw.pipelines.backdrop_crop_vertex,
            &draw.pipelines.backdrop_crop_fragment,
            &draw.pipelines.blur_blend_replace,
            &crop.rtv,
            &source_srv,
            BlurUniforms::copy(crop.total),
            &D3D11_VIEWPORT {
                TopLeftX: 0.0,
                TopLeftY: 0.0,
                Width: crop.total[0],
                Height: crop.total[1],
                MinDepth: 0.0,
                MaxDepth: 1.0,
            },
            D3D_PRIMITIVE_TOPOLOGY_TRIANGLELIST,
            3,
            true,
            BlurPassExtras::Crop(&crop_uniforms),
        )?;
        blur_source = crop.srv.clone();
        source_size = crop.total;
        blur_size = [
            downsampled_dimension(crop.total[0] as u32) as f32,
            downsampled_dimension(crop.total[1] as u32) as f32,
        ];
        ping_rtv = crop.half_a_rtv.clone();
        ping_srv = crop.half_a_srv.clone();
        pong_rtv = crop.half_b_rtv.clone();
        pong_srv = crop.half_b_srv.clone();
        // Region-relative sampling: uv = (position - (origin - margin))
        // / total.
        extensions.sample_offset = [
            crop.origin[0] - crop.margin[0],
            crop.origin[1] - crop.margin[1],
        ];
    }

    let half_vp = D3D11_VIEWPORT {
        TopLeftX: 0.0,
        TopLeftY: 0.0,
        Width: blur_size[0],
        Height: blur_size[1],
        MinDepth: 0.0,
        MaxDepth: 1.0,
    };

    // Downsample source -> ping, then separable gaussian ping -> pong ->
    // ping. A matrix-only chain (no blur kernel) composites the source
    // directly. rev 16: the half pair comes from the crop (backdrops
    // always crop), so blur_size and the pair agree.
    let composited_srv: Option<ID3D11ShaderResourceView>;
    if let Some(kernel) = kernel {
        if ping_rtv.is_none() {
            // A blurred backdrop without a crop pair cannot run the
            // chain (the crop always allocates one; defensive).
            return Err(renderer_status::ERR_DEVICE);
        }
        dx_blur_pass(
            draw,
            frame,
            &draw.pipelines.blur_downsample_vertex,
            &draw.pipelines.blur_downsample_fragment,
            &draw.pipelines.blur_blend_replace,
            &ping_rtv,
            &blur_source,
            BlurUniforms::downsample(source_size, blur_size),
            &half_vp,
            D3D_PRIMITIVE_TOPOLOGY_TRIANGLELIST,
            3,
            true,
            BlurPassExtras::None,
        )?;
        dx_blur_pass(
            draw,
            frame,
            &draw.pipelines.blur_vertex,
            &draw.pipelines.blur_fragment,
            &draw.pipelines.blur_blend_replace,
            &pong_rtv,
            &ping_srv,
            BlurUniforms::gaussian(BlurAxis::Horizontal, blur_size, kernel),
            &half_vp,
            D3D_PRIMITIVE_TOPOLOGY_TRIANGLELIST,
            3,
            true,
            BlurPassExtras::None,
        )?;
        dx_blur_pass(
            draw,
            frame,
            &draw.pipelines.blur_vertex,
            &draw.pipelines.blur_fragment,
            &draw.pipelines.blur_blend_replace,
            &ping_rtv,
            &pong_srv,
            BlurUniforms::gaussian(BlurAxis::Vertical, blur_size, kernel),
            &half_vp,
            D3D_PRIMITIVE_TOPOLOGY_TRIANGLELIST,
            3,
            true,
            BlurPassExtras::None,
        )?;
        composited_srv = ping_srv;
    } else if rec.color_matrix_count > 0 {
        composited_srv = blur_source;
    } else {
        // The pinned early-out: a zero-radius blur chain with no matrix
        // has nothing to draw.
        return Ok(());
    }

    // Content blur bleeds ~3*radius past the box; composite over a
    // dilated rect. Backdrops clip to their (rounded) bounds.
    let composite_bounds = if clip_rounded {
        bounds
    } else {
        bounds.dilate(gpui::ScaledPixels(
            GAUSSIAN_CUTOFF_STANDARD_DEVIATIONS * blur_radius,
        ))
    };
    let composite_uniforms = BlurUniforms::composite(
        composite_bounds,
        content_mask,
        corner_radii,
        corner_smoothing,
        opacity,
        clip,
        blur_size,
        source_size,
    );
    let (composite_vertex, composite_fragment) = if composite_uniforms.corner_smoothing > 0.0 {
        (
            &draw.pipelines.smoothed_blur_composite_vertex,
            &draw.pipelines.smoothed_blur_composite_fragment,
        )
    } else {
        (
            &draw.pipelines.blur_composite_vertex,
            &draw.pipelines.blur_composite_fragment,
        )
    };
    // Composite the result into the target (preserving its contents),
    // applying the composed colour matrix.
    dx_blur_pass(
        draw,
        frame,
        composite_vertex,
        composite_fragment,
        &draw.pipelines.blur_blend_composite,
        &target_rtv,
        &composited_srv,
        composite_uniforms,
        &full_vp,
        D3D_PRIMITIVE_TOPOLOGY_TRIANGLESTRIP,
        4,
        false,
        BlurPassExtras::Composite(&extensions),
    )?;
    Ok(())
}

/// Allocates (or grows) the surface's backdrop crop texture for one
/// backdrop's region plus margin and returns the crop coordinates. The
/// region covers the element's pixel-covered bounds; the margin is the
/// blur kernel's reach (so the kernel's reads past the region fold back
/// onto the element's own mirrored content), clamped so the crop stays
/// within the surface.
fn ensure_backdrop_crop(
    state: &mut RendererState,
    slot_index: usize,
    slot_generation: u32,
    bounds: gpui::Bounds<gpui::ScaledPixels>,
    blur_radius: f32,
    source_size: [f32; 2],
) -> Result<BackdropCropRegion, i32> {
    let origin = [
        bounds.origin.x.0.floor().max(0.0),
        bounds.origin.y.0.floor().max(0.0),
    ];
    let region = [
        (bounds.origin.x.0 + bounds.size.width.0).ceil().max(0.0) - origin[0],
        (bounds.origin.y.0 + bounds.size.height.0).ceil().max(0.0) - origin[1],
    ];
    // The kernel's reach at half resolution is 3*sigma = 1.5*radius
    // half-res texels = 3*radius full-res pixels; two extra pixels of
    // slack cover the rounding.
    let reach = (GAUSSIAN_CUTOFF_STANDARD_DEVIATIONS * blur_radius).ceil() + 2.0;
    let (full_width, full_height) = {
        let surface_state = resolve_slot_mut(state, slot_index, slot_generation)?;
        (surface_state.width, surface_state.height)
    };
    let mut margin = [reach, reach];
    // Clamp the crop inside its blur source (the CURRENT target: the
    // window, or the group bucket a backdrop inside an isolated group
    // samples - rev 16, ticket 51: the bucket is bounds-sized, so
    // clamping to the window would let the crop read past the bucket).
    // A heavily blurred element near the edge gets a truncated margin;
    // the blur's wrap reads there are discarded by the
    // region-relative composite anyway.
    let source_width = if source_size[0] > 0.0 {
        source_size[0]
    } else {
        full_width as f32
    };
    let source_height = if source_size[1] > 0.0 {
        source_size[1]
    } else {
        full_height as f32
    };
    margin[0] = margin[0].min(origin[0]).min((source_width - origin[0] - region[0]).max(0.0));
    margin[1] = margin[1].min(origin[1]).min((source_height - origin[1] - region[1]).max(0.0));
    let total = [region[0] + 2.0 * margin[0], region[1] + 2.0 * margin[1]];
    let total_w = (total[0].ceil() as u32).max(1);
    let total_h = (total[1].ceil() as u32).max(1);

    let device = ensure_device(state)?.device;
    let surface_state = resolve_slot_mut(state, slot_index, slot_generation)?;
    let needs_new = surface_state
        .draw_targets
        .backdrop_crop
        .as_ref()
        .is_none_or(|crop| crop.width != total_w || crop.height != total_h);
    if needs_new {
        let (_, rtv, srv) = create_color_target(&device, total_w, total_h)?;
        // rev 16 (ticket 50, folding in ticket 77's fix): the crop's
        // own half-resolution blur pair - the gaussian passes' uv math
        // assumes the scratch matches the blur source's size.
        let half_w = downsampled_dimension(total_w);
        let half_h = downsampled_dimension(total_h);
        let (_, half_a_rtv, half_a_srv) = create_color_target(&device, half_w, half_h)?;
        let (_, half_b_rtv, half_b_srv) = create_color_target(&device, half_w, half_h)?;
        surface_state.draw_targets.backdrop_crop = Some(BackdropCropTarget {
            rtv: Some(rtv),
            srv: Some(srv),
            width: total_w,
            height: total_h,
            half_a_rtv,
            half_a_srv,
            half_b_rtv,
            half_b_srv,
        });
    }
    let surface_state = resolve_slot_mut(state, slot_index, slot_generation)?;
    let crop = surface_state
        .draw_targets
        .backdrop_crop
        .as_ref()
        .ok_or(renderer_status::ERR_DEVICE)?;
    Ok(BackdropCropRegion {
        origin,
        region,
        margin,
        total: [total_w as f32, total_h as f32],
        rtv: crop.rtv.clone().flatten(),
        srv: crop.srv.clone().flatten(),
        half_a_rtv: crop.half_a_rtv.clone(),
        half_a_srv: crop.half_a_srv.clone(),
        half_b_rtv: crop.half_b_rtv.clone(),
        half_b_srv: crop.half_b_srv.clone(),
    })
}

/// Copy the offscreen scene texture into the swapchain render target
/// (the pin's `dx_blit`).
fn dx_blit(
    state: &mut RendererState,
    draw: &DrawGlobal,
    slot_index: usize,
    slot_generation: u32,
    frame: &FrameBindings<'_>,
    source_srv: &Option<ID3D11ShaderResourceView>,
    target_rtv: &Option<ID3D11RenderTargetView>,
) -> Result<(), i32> {
    let (full_width, full_height, full_vp) = {
        let surface_state = resolve_slot(state, slot_index, slot_generation)?;
        (surface_state.width, surface_state.height, surface_state.viewport)
    };
    let target = target_rtv.clone();
    dx_blur_pass(
        draw,
        frame,
        &draw.pipelines.blur_downsample_vertex,
        &draw.pipelines.blur_downsample_fragment,
        &draw.pipelines.blur_blend_replace,
        &target,
        source_srv,
        BlurUniforms::copy([full_width as f32, full_height as f32]),
        &full_vp,
        D3D_PRIMITIVE_TOPOLOGY_TRIANGLELIST,
        3,
        true,
        BlurPassExtras::None,
    )
}

// ---------------------------------------------------------------------------
// Export bodies (ticket16)
// ---------------------------------------------------------------------------

/// Maps a scene-snapshot failure to the renderer status (one typed
/// code: the scene handle did not resolve to a finished, healthy
/// scene).
fn map_scene_status(_code: i32) -> i32 {
    renderer_status::ERR_SCENE
}

/// The render half of the draw entry (no present, no submission): the
/// pin's `render` body, ending before its `present`.
fn render_scene_entry(
    surface: u64,
    scene: u64,
    appearance: u32,
    atlas: u64,
) -> Result<(), i32> {
    if appearance != background_appearance::OPAQUE
        && appearance != background_appearance::TRANSPARENT
    {
        return Err(renderer_status::ERR_BAD_VALUE);
    }
    // Snapshot the scene FIRST (its own lock; no lock nesting with the
    // renderer state — the snapshot owns every byte).
    let snapshot: SceneRenderSnapshot =
        scene_render_snapshot(scene).map_err(map_scene_status)?;

    let mut state = lock_global();
    // Ticket29: the recovery gate (the pin's skip_draws in `draw` —
    // the skip is observable here instead of silent).
    if state.recovering {
        return Err(renderer_status::ERR_RECOVERING);
    }
    ensure_device(&mut state)?;
    ensure_draw_global(&mut state)?;
    let mut lease = DrawLease::take(&mut state)?;
    let (slot_index, slot_generation) = super::surface_handle_parts(surface);
    let DrawLease { state, value } = &mut lease;
    let draw = value.as_mut().ok_or(renderer_status::ERR_DEVICE)?;
    render_scene(
        state,
        draw,
        slot_index,
        slot_generation,
        &snapshot,
        atlas,
        appearance,
    )?;
    Ok(())
}

/// `renderer_surface_draw_scene`: render the finished scene into the
/// surface's back buffer, present it, and record the tracked submission
/// (the pin's `draw` = render + present, with the ticket07 event-query
/// retirement).
pub(super) fn surface_draw_scene_body(
    surface: u64,
    scene: u64,
    appearance: u32,
    atlas: u64,
    out_submission: *mut u64,
) -> i32 {
    if out_submission.is_null() {
        return renderer_status::ERR_NULL_ARG;
    }
    let result = catch_unwind(AssertUnwindSafe(|| {
        render_scene_entry(surface, scene, appearance, atlas)?;
        submit_draw_frame(surface, out_submission)
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

/// `renderer_surface_render_scene`: render without presenting (the
/// pin's `render`, the readback test path — no submission token).
pub(super) fn surface_render_scene_body(
    surface: u64,
    scene: u64,
    appearance: u32,
    atlas: u64,
) -> i32 {
    let result = catch_unwind(AssertUnwindSafe(|| {
        render_scene_entry(surface, scene, appearance, atlas)?;
        Ok::<(), i32>(())
    }));
    match result {
        Ok(Ok(())) => renderer_status::OK,
        Ok(Err(code)) => code,
        Err(payload) => {
            drop(payload);
            renderer_status::ERR_PANIC
        }
    }
}

/// The present + submission-record tail (mirrors `surface_present`'s:
/// bounded pending, Present(0, 0), a `D3D11_QUERY_EVENT` ended after the
/// last command, one explicit `Flush` so the marker is actually
/// submitted, and a tracked partial-submit on present failure).
fn submit_draw_frame(surface: u64, out_submission: *mut u64) -> Result<i32, i32> {
    let (slot_index, slot_generation) = super::surface_handle_parts(surface);
    let mut state = lock_global();
    let device = ensure_device(&mut state)?;
    let surface_state = resolve_slot_mut(&mut state, slot_index, slot_generation)?;
    // Bounded pending submissions: backpressure BEFORE accepting GPU work.
    if surface_state.submissions.len() >= super::MAX_PENDING_SUBMISSIONS as usize {
        return Ok(renderer_status::ERR_BUSY);
    }
    // Sequence space exhaustion (2^32 per surface) is a hard error.
    match surface_state.next_seq.checked_add(1) {
        Some(next) if next <= super::SUBMISSION_SEQ_MASK => {}
        _ => return Ok(renderer_status::ERR_BAD_VALUE),
    }
    let present_hr = unsafe { surface_state.swap_chain.Present(0, DXGI_PRESENT(0)) };

    // Completion marker: a D3D11_QUERY_EVENT ended AFTER the last GPU
    // command of the submission, then ONE explicit Flush so the marker
    // is actually submitted (each submission here is the "rendering goes
    // idle" case; Present and Flush are submission aids, never
    // completion evidence: only a later poll's S_OK retires).
    let query = {
        let desc = D3D11_QUERY_DESC {
            Query: D3D11_QUERY_EVENT,
            MiscFlags: 0,
        };
        let mut query: Option<ID3D11Query> = None;
        match unsafe { device.device.CreateQuery(&desc, Some(&mut query)) } {
            Ok(()) => match query {
                Some(query) => query,
                None => {
                    if present_hr.is_err() {
                        return Ok(renderer_status::ERR_PRESENT);
                    }
                    return Ok(renderer_status::ERR_DEVICE);
                }
            },
            Err(_) => {
                if present_hr.is_err() {
                    return Ok(renderer_status::ERR_PRESENT);
                }
                return Ok(renderer_status::ERR_DEVICE);
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
    // Partial-submit path: a failed Present still tracks the submission.
    surface_state.submissions.insert(
        sequence,
        super::SubmissionRecord {
            sequence,
            query: Some(query),
            state: super::SubmissionState::Pending,
            present_hr: present_hr.0,
            data_hr: 0,
        },
    );
    let handle = super::make_submission_handle(slot_index, slot_generation, sequence);
    unsafe { *out_submission = handle };
    if present_hr.is_err() {
        return Ok(renderer_status::ERR_PRESENT);
    }
    Ok(renderer_status::OK)
}

/// `renderer_surface_render_to_image` (ticket32): the pinned
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
/// texture, `CopyResource`, `Map(READ)`, row copy, BGRA->RGBA swap).
pub(super) fn surface_read_pixels_body(
    surface: u64,
    out_bytes: *mut u8,
    capacity: u32,
    out_needed: *mut u32,
) -> i32 {
    if out_needed.is_null() {
        return renderer_status::ERR_NULL_ARG;
    }
    let result = catch_unwind(AssertUnwindSafe(|| {
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
        // readable bytes until `Unmap`; each destination row is disjoint
        // within the exactly-sized output allocation.
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
