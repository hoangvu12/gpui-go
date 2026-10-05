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

use std::panic::{catch_unwind, AssertUnwindSafe};
use std::slice;
use std::sync::OnceLock;

use gpui_render::{
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
    SceneRenderSnapshot, SceneRenderSnapshot as Snapshot, batch_primitive_kind, command_kind,
    scene_render_snapshot,
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

/// Group 0 occupies registers 0 and 1 in native shaders, so generated
/// group-1 bindings start at 2 (the pin's `GROUP_1_REGISTER_OFFSET`).
const GROUP_1_REGISTER_OFFSET: u32 = 2;
const DATA_REGISTER: u32 = shader_interface::DATA_BUFFER_BINDING + GROUP_1_REGISTER_OFFSET;
const PRIMARY_TEXTURE_REGISTER: u32 =
    shader_interface::PRIMARY_TEXTURE_BINDING + GROUP_1_REGISTER_OFFSET;
const PRIMARY_SAMPLER_REGISTER: u32 =
    shader_interface::PRIMARY_SAMPLER_BINDING + GROUP_1_REGISTER_OFFSET;
/// The surface sampler register of the SURFACES pipeline — reserved for
/// the imported-surface draw path (the capture-producer slice).
#[expect(dead_code)]
const SURFACE_SAMPLER_REGISTER: u32 =
    shader_interface::SURFACE_SAMPLER_BINDING + GROUP_1_REGISTER_OFFSET;

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
    /// is intentionally unsupported by the pinned artifact).
    fn bytecode(self) -> Result<gpui_render::artifacts::Dx11Bytecode, i32> {
        match self.native_shader().dx11 {
            Dx11Shader::Sm50(bytecode) => Ok(bytecode),
            Dx11Shader::NativeWindowsBuildRequired => Err(renderer_status::ERR_DEVICE),
        }
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
        let sampler = unsafe {
            let desc = D3D11_SAMPLER_DESC {
                Filter: D3D11_FILTER_MIN_MAG_MIP_LINEAR,
                AddressU: D3D11_TEXTURE_ADDRESS_WRAP,
                AddressV: D3D11_TEXTURE_ADDRESS_WRAP,
                AddressW: D3D11_TEXTURE_ADDRESS_WRAP,
                MipLODBias: 0.0,
                MaxAnisotropy: 1,
                ComparisonFunc: D3D11_COMPARISON_ALWAYS,
                BorderColor: [0.0; 4],
                MinLOD: 0.0,
                MaxLOD: D3D11_FLOAT32_MAX,
            };
            let mut output = None;
            device.CreateSamplerState(&desc, Some(&mut output)).map_err(|e| e.code().0)?;
            output.ok_or(renderer_status::ERR_DEVICE)?
        };
        Ok(Self {
            globals_buffer,
            font_buffer,
            draw_constants_buffer,
            sampler: Some(sampler),
        })
    }
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
            .ok_or(renderer_status::ERR_DEVICE)?
            .register;
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
            .ok_or(renderer_status::ERR_DEVICE)?
            .register;
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
        let vertex_count = self
            .specification
            .vertex_count
            .fixed()
            .ok_or(renderer_status::ERR_DEVICE)?;
        self.draw_with_variant(frame, texture, vertex_count, first, count, false)
    }

    /// The pin's `draw_instances_variant` (the smoothed variant toggle).
    fn draw_instances_variant(
        &self,
        frame: &FrameBindings<'_>,
        texture: Option<&[Option<ID3D11ShaderResourceView>; 1]>,
        first: u32,
        count: u32,
        use_variant: bool,
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
        self.draw_with_variant(frame, texture, vertex_count, first, count, use_variant)
    }

    /// The pin's `draw_vertices` (vertex-pulled, one instance).
    fn draw_vertices(&self, frame: &FrameBindings<'_>, vertex_count: u32) -> Result<(), i32> {
        if self.specification.vertex_count.fixed().is_some() {
            return Err(renderer_status::ERR_DEVICE);
        }
        self.draw_with_variant(frame, None, vertex_count, 0, 1, false)
    }

    /// The pin's `draw_with_variant`: the single place that issues an
    /// instanced draw. The batch base reaches the shader through the
    /// draw-constants cbuffer (`StartInstanceLocation` stays zero, the
    /// shader adds the base itself).
    fn draw_with_variant(
        &self,
        frame: &FrameBindings<'_>,
        texture: Option<&[Option<ID3D11ShaderResourceView>; 1]>,
        vertex_count: u32,
        first: u32,
        count: u32,
        use_variant: bool,
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
        let draw_constants = [Some(frame.globals.draw_constants_buffer.clone())];
        let view = [self.view.clone()];
        unsafe {
            ctx.VSSetShaderResources(DATA_REGISTER, Some(&view));
            ctx.PSSetShaderResources(DATA_REGISTER, Some(&view));
            ctx.IASetPrimitiveTopology(topology);
            ctx.RSSetViewports(Some(slice::from_ref(&frame.viewport)));
            if use_variant {
                let variant = self.variant.as_ref().ok_or(renderer_status::ERR_DEVICE)?;
                ctx.VSSetShader(&variant.vertex, None);
                ctx.PSSetShader(&variant.fragment, None);
            } else {
                ctx.VSSetShader(&self.vertex, None);
                ctx.PSSetShader(&self.fragment, None);
            }
            ctx.VSSetConstantBuffers(0, Some(&frame.globals.cbuffers()));
            ctx.PSSetConstantBuffers(0, Some(&frame.globals.cbuffers()));
            ctx.VSSetConstantBuffers(self.draw_constants_register, Some(&draw_constants));
            ctx.OMSetBlendState(&self.blend_state, None, 0xFFFFFFFF);
            if let Some(texture) = texture {
                ctx.PSSetSamplers(PRIMARY_SAMPLER_REGISTER, Some(slice::from_ref(&frame.globals.sampler)));
                // The vertex stage reads the atlas dimensions for tile
                // coordinates.
                ctx.VSSetShaderResources(PRIMARY_TEXTURE_REGISTER, Some(texture));
                ctx.PSSetShaderResources(PRIMARY_TEXTURE_REGISTER, Some(texture));
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
/// is the `SurfaceSource::Unsupported` stand-in, so the draw path takes
/// the pinned import-error branch before binding these (they await the
/// capture-producer slice).
#[expect(dead_code)]
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
    /// Constructed like the pin's; the draw path takes the pinned
    /// import-error branch for the ABI's Unsupported surface sources
    /// before binding it.
    #[expect(dead_code)]
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
    blur_params_buffer: ID3D11Buffer,
    blur_blend_replace: ID3D11BlendState,
    blur_blend_composite: ID3D11BlendState,
}

impl DrawPipelines {
    fn new(device: &ID3D11Device) -> Result<Self, i32> {
        let shadow_pipeline = InstancePipeline::new(
            device,
            "shadow_pipeline",
            ShaderModule::Shadow,
            std::mem::size_of::<gpui::Shadow>(),
            4,
            create_blend_state(device)?,
        )?
        .with_variant(device, ShaderModule::SmoothedShadow)?;
        let quad_pipeline = InstancePipeline::new(
            device,
            "quad_pipeline",
            ShaderModule::Quad,
            std::mem::size_of::<gpui::Quad>(),
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
        let blur_params_buffer =
            create_constant_buffer(device, std::mem::size_of::<BlurUniforms>())?;
        let blur_blend_replace = create_blend_state_no_blend(device)?;
        // Premultiplied (One / InvSrcAlpha) — the composite outputs a
        // premultiplied blurred sample; straight-alpha blending would
        // darken the faded edges.
        let blur_blend_composite = create_blend_state_for_path_sprite(device)?;

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
            blur_params_buffer,
            blur_blend_replace,
            blur_blend_composite,
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

/// The pin's `BlurResources`: the offscreen scene color, the
/// half-resolution ping/pong scratch, and one isolated group target per
/// nesting depth (up to `MAX_FILTER_GROUP_DEPTH`; deeper groups render
/// inline).
pub(super) struct BlurResources {
    scene_color_rtv: Option<ID3D11RenderTargetView>,
    scene_color_srv: Option<ID3D11ShaderResourceView>,
    ping_rtv: Option<ID3D11RenderTargetView>,
    ping_srv: Option<ID3D11ShaderResourceView>,
    pong_rtv: Option<ID3D11RenderTargetView>,
    pong_srv: Option<ID3D11ShaderResourceView>,
    group_rtvs: Vec<Option<ID3D11RenderTargetView>>,
    group_srvs: Vec<Option<ID3D11ShaderResourceView>>,
}

impl BlurResources {
    fn new(device: &ID3D11Device, width: u32, height: u32, isolated_target_count: usize) -> Result<Self, i32> {
        let half_w = downsampled_dimension(width);
        let half_h = downsampled_dimension(height);
        let (_, scene_color_rtv, scene_color_srv) = create_color_target(device, width, height)?;
        let (_, ping_rtv, ping_srv) = create_color_target(device, half_w, half_h)?;
        let (_, pong_rtv, pong_srv) = create_color_target(device, half_w, half_h)?;
        let mut group_rtvs = Vec::with_capacity(isolated_target_count);
        let mut group_srvs = Vec::with_capacity(isolated_target_count);
        for _ in 0..isolated_target_count {
            let (_, rtv, srv) = create_color_target(device, width, height)?;
            group_rtvs.push(rtv);
            group_srvs.push(srv);
        }
        Ok(Self {
            scene_color_rtv,
            scene_color_srv,
            ping_rtv,
            ping_srv,
            pong_rtv,
            pong_srv,
            group_rtvs,
            group_srvs,
        })
    }

    fn ensure_isolated_targets(
        &mut self,
        device: &ID3D11Device,
        width: u32,
        height: u32,
        isolated_target_count: usize,
    ) -> Result<(), i32> {
        while self.group_rtvs.len() < isolated_target_count {
            let (_, rtv, srv) = create_color_target(device, width, height)?;
            self.group_rtvs.push(rtv);
            self.group_srvs.push(srv);
        }
        Ok(())
    }
}

/// The per-surface, size-dependent draw targets (dropped on resize like
/// the pin's `recreate_resources`).
#[derive(Default)]
pub(super) struct DrawTargets {
    pub(super) path: Option<PathResources>,
    pub(super) blur: Option<BlurResources>,
}

// ---------------------------------------------------------------------------
// Frame-wide bindings (the pin's FrameBindings)
// ---------------------------------------------------------------------------

/// Frame-wide state that every batch draw binds alongside its own
/// pipeline (the pin's `FrameBindings`, owning a clone of the global
/// elements so the draw walk can mutably update the instance buffers).
struct FrameBindings<'a> {
    device_context: &'a ID3D11DeviceContext,
    viewport: D3D11_VIEWPORT,
    globals: GlobalElements,
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

    pre_draw(&ctx, &draw.globals, &viewport, &swapchain_rtv, appearance)?;
    upload_scene_buffers(&device, &ctx, draw, snapshot)?;

    // Only route through the offscreen scene texture when the scene
    // contains blur filters; otherwise render straight to the swapchain
    // exactly as before.
    let use_offscreen = snapshot.requirements.uses_offscreen_target != 0;
    if snapshot.requirements.uses_path_target != 0 {
        ensure_path_resources(state, slot_index, slot_generation, &device)?;
    }
    let isolated_target_count = snapshot.requirements.isolated_target_count as usize;
    if use_offscreen {
        ensure_blur_resources(state, slot_index, slot_generation, &device, isolated_target_count)?;
    }

    // Clone the views we need (AddRef) so the loop can rebind render
    // targets without borrow tangles.
    let (scene_rtv, scene_srv, group_rtvs, group_srvs) = {
        let surface_state = resolve_slot(state, slot_index, slot_generation)?;
        match surface_state.draw_targets.blur.as_ref() {
            Some(blur) => (
                blur.scene_color_rtv.clone(),
                blur.scene_color_srv.clone(),
                blur.group_rtvs.clone(),
                blur.group_srvs.clone(),
            ),
            None => {
                if use_offscreen {
                    return Err(renderer_status::ERR_DEVICE);
                }
                (None, None, Vec::new(), Vec::new())
            }
        }
    };

    let mut active_render_target: Option<ID3D11RenderTargetView>;
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

    // Current target for the main scene + a parent stack for
    // content-filter groups.
    let mut current_rtv = active_render_target.clone();
    let mut current_srv = if use_offscreen {
        scene_srv.clone()
    } else {
        None
    };
    let mut filter_stack: Vec<(
        Option<ID3D11RenderTargetView>,
        Option<ID3D11ShaderResourceView>,
    )> = Vec::new();

    let frame = FrameBindings {
        device_context: &ctx,
        viewport,
        globals: draw.globals.clone(),
    };

    let mut path_rasterization_vertices: Vec<PathRasterizationVertex> = Vec::new();
    let mut path_sprites: Vec<PathSprite> = Vec::new();

    for command in &snapshot.commands {
        if command.command_kind == command_kind::BATCH {
            let range = command.range_start as usize..command.range_end as usize;
            match command.primitive_kind {
                batch_primitive_kind::SHADOWS => {
                    let instances = instance_range(command.range_start, command.range_end)?;
                    draw.pipelines.shadow_pipeline.draw_instances_variant(
                        &frame,
                        None,
                        instances.first(),
                        instances.count(),
                        command.smoothed != 0,
                    )?;
                }
                batch_primitive_kind::QUADS => {
                    let instances = instance_range(command.range_start, command.range_end)?;
                    draw.pipelines.quad_pipeline.draw_instances_variant(
                        &frame,
                        None,
                        instances.first(),
                        instances.count(),
                        command.smoothed != 0,
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
                    draw.pipelines.mono_sprites.draw_instances(
                        &frame,
                        Some(&texture),
                        instances.first(),
                        instances.count(),
                    )?;
                }
                batch_primitive_kind::SUBPIXEL_SPRITES => {
                    let texture = sprite_texture(atlas, 2, command.texture_index)?;
                    let instances = instance_range(command.range_start, command.range_end)?;
                    draw.pipelines.subpixel_sprites.draw_instances(
                        &frame,
                        Some(&texture),
                        instances.first(),
                        instances.count(),
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
                    )?;
                }
                batch_primitive_kind::SURFACES => {
                    draw_surfaces(
                        &snapshot.surfaces[range.clone()],
                    )?;
                    // Restore the current target for subsequent batches
                    // (the pinned loop's post-draw rebinding).
                    unsafe {
                        ctx.OMSetRenderTargets(Some(slice::from_ref(&current_rtv)), None);
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
                            &current_srv,
                            &current_rtv,
                            filter.bounds,
                            filter.content_mask.bounds,
                            filter.corner_radii,
                            filter.corner_smoothing,
                            filter.max_blur_radius(),
                            filter.opacity,
                            // Backdrop clips to the rounded rect.
                            true,
                        )?;
                    }
                    // Restore the current target for subsequent batches.
                    unsafe {
                        ctx.OMSetRenderTargets(Some(slice::from_ref(&current_rtv)), None);
                    }
                }
                _ => {
                    // The filter-boundary tag never appears as a batch
                    // command (the plan compiles boundaries into
                    // begin/end commands).
                }
            }
        } else if command.command_kind == command_kind::BEGIN_FILTER
            && command.filter_target == 1
        {
            // BeginFilter { target: Isolated(index) }: redirect rendering
            // into the group target.
            let index = command.target_index as usize;
            if index >= group_rtvs.len() {
                return Err(renderer_status::ERR_DEVICE);
            }
            filter_stack.push((current_rtv.clone(), current_srv.clone()));
            current_rtv = group_rtvs[index].clone();
            current_srv = group_srvs[index].clone();
            active_render_target = current_rtv.clone();
            unsafe {
                if let Some(rtv) = current_rtv.as_ref() {
                    ctx.ClearRenderTargetView(rtv, &[0.0; 4]);
                    let bound = Some(rtv.clone());
                    ctx.OMSetRenderTargets(Some(slice::from_ref(&bound)), None);
                }
            }
        } else if command.command_kind == command_kind::END_FILTER
            && command.filter_target == 1
        {
            // EndFilter { target: Isolated(_) }: composite the group into
            // its parent.
            let boundary = snapshot
                .boundaries
                .get(command.boundary_index as usize)
                .ok_or(renderer_status::ERR_BAD_VALUE)?;
            let (parent_rtv, parent_srv) = filter_stack
                .pop()
                .ok_or(renderer_status::ERR_DEVICE)?;
            dx_blur_and_composite(
                state,
                draw,
                slot_index,
                slot_generation,
                &frame,
                &current_srv,
                &parent_rtv,
                boundary.bounds,
                boundary.content_mask.bounds,
                boundary.corner_radii,
                boundary.corner_smoothing,
                boundary.max_blur_radius(),
                boundary.opacity,
                // Content (`filter`) bleeds past its bounds.
                false,
            )?;
            current_rtv = parent_rtv;
            current_srv = parent_srv;
            active_render_target = current_rtv.clone();
            unsafe {
                ctx.OMSetRenderTargets(Some(slice::from_ref(&current_rtv)), None);
            }
        }
        // Inline filter groups and inline begin/end commands draw
        // nothing (the pinned plan's Inline arms are no-ops).
    }

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
    Ok(())
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
    Ok(())
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

fn ensure_blur_resources(
    state: &mut RendererState,
    slot_index: usize,
    slot_generation: u32,
    device: &ID3D11Device,
    isolated_target_count: usize,
) -> Result<(), i32> {
    let surface_state = resolve_slot_mut(state, slot_index, slot_generation)?;
    if surface_state.draw_targets.blur.is_none() {
        let width = surface_state.width;
        let height = surface_state.height;
        surface_state.draw_targets.blur = Some(BlurResources::new(
            device,
            width,
            height,
            isolated_target_count,
        )?);
    }
    let surface_state = resolve_slot_mut(state, slot_index, slot_generation)?;
    let width = surface_state.width;
    let height = surface_state.height;
    let blur = surface_state
        .draw_targets
        .blur
        .as_mut()
        .ok_or(renderer_status::ERR_DEVICE)?;
    blur.ensure_isolated_targets(device, width, height, isolated_target_count)
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

/// The pin's `draw_surfaces` import path: the pinned renderer imports
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
) -> Result<(), i32> {
    let ctx = frame.device_context;
    update_buffer(ctx, &draw.pipelines.blur_params_buffer, &[params])?;
    let null_srv: [Option<ID3D11ShaderResourceView>; 1] = [None];
    let cbuffers = frame.globals.cbuffers();
    let blur_params = [Some(draw.pipelines.blur_params_buffer.clone())];
    unsafe {
        // Unbind any SRV at the blur slot; the target must not be bound
        // as input.
        ctx.PSSetShaderResources(PRIMARY_TEXTURE_REGISTER, Some(&null_srv));
        if clear {
            let rtv = target_rtv.as_ref().ok_or(renderer_status::ERR_DEVICE)?;
            ctx.ClearRenderTargetView(rtv, &[0.0; 4]);
        }
        ctx.OMSetRenderTargets(Some(slice::from_ref(target_rtv)), None);
        ctx.RSSetViewports(Some(slice::from_ref(viewport)));
        ctx.IASetPrimitiveTopology(topology);
        ctx.VSSetShader(vertex, None);
        ctx.PSSetShader(fragment, None);
        ctx.VSSetConstantBuffers(0, Some(&cbuffers));
        ctx.PSSetConstantBuffers(0, Some(&cbuffers));
        ctx.VSSetConstantBuffers(DATA_REGISTER, Some(&blur_params));
        ctx.PSSetConstantBuffers(DATA_REGISTER, Some(&blur_params));
        ctx.PSSetSamplers(
            PRIMARY_SAMPLER_REGISTER,
            Some(slice::from_ref(&frame.globals.sampler)),
        );
        ctx.PSSetShaderResources(PRIMARY_TEXTURE_REGISTER, Some(slice::from_ref(source_srv)));
        ctx.OMSetBlendState(blend, None, 0xFFFFFFFF);
        ctx.DrawInstanced(vertex_count, 1, 0, 0);
        // Unbind the source so the target can be rebound as a render
        // target next.
        ctx.PSSetShaderResources(PRIMARY_TEXTURE_REGISTER, Some(&null_srv));
    }
    Ok(())
}

/// Blur `source_srv` through the half-res ping/pong textures and
/// composite the result into `target_rtv` (the pin's
/// `dx_blur_and_composite`, shared by the backdrop and content-filter
/// paths).
#[allow(clippy::too_many_arguments)]
fn dx_blur_and_composite(
    state: &mut RendererState,
    draw: &DrawGlobal,
    slot_index: usize,
    slot_generation: u32,
    frame: &FrameBindings<'_>,
    source_srv: &Option<ID3D11ShaderResourceView>,
    target_rtv: &Option<ID3D11RenderTargetView>,
    bounds: gpui::Bounds<gpui::ScaledPixels>,
    content_mask: gpui::Bounds<gpui::ScaledPixels>,
    corner_radii: gpui::Corners<gpui::ScaledPixels>,
    corner_smoothing: f32,
    blur_radius: f32,
    opacity: f32,
    // Backdrop clips to the rounded rect; content (`filter`) bleeds past
    // its bounds.
    clip_rounded: bool,
) -> Result<(), i32> {
    let (full_width, full_height, full_vp) = {
        let surface_state = resolve_slot(state, slot_index, slot_generation)?;
        (surface_state.width, surface_state.height, surface_state.viewport)
    };
    let blur_size = [
        downsampled_dimension(full_width) as f32,
        downsampled_dimension(full_height) as f32,
    ];
    let clip = if clip_rounded {
        FilterCompositeClip::RoundedBounds
    } else {
        FilterCompositeClip::ContentShape
    };
    let half_vp = D3D11_VIEWPORT {
        TopLeftX: 0.0,
        TopLeftY: 0.0,
        Width: blur_size[0],
        Height: blur_size[1],
        MinDepth: 0.0,
        MaxDepth: 1.0,
    };
    let (ping_rtv, ping_srv, pong_rtv, pong_srv) = {
        let surface_state = resolve_slot(state, slot_index, slot_generation)?;
        let blur = surface_state
            .draw_targets
            .blur
            .as_ref()
            .ok_or(renderer_status::ERR_DEVICE)?;
        (
            blur.ping_rtv.clone(),
            blur.ping_srv.clone(),
            blur.pong_rtv.clone(),
            blur.pong_srv.clone(),
        )
    };
    let Some(kernel) = BlurKernel::for_radius(blur_radius) else {
        return Ok(());
    };

    // Downsample source -> ping, then separable gaussian ping -> pong ->
    // ping.
    dx_blur_pass(
        draw,
        frame,
        &draw.pipelines.blur_downsample_vertex,
        &draw.pipelines.blur_downsample_fragment,
        &draw.pipelines.blur_blend_replace,
        &ping_rtv,
        source_srv,
        BlurUniforms::downsample([full_width as f32, full_height as f32], blur_size),
        &half_vp,
        D3D_PRIMITIVE_TOPOLOGY_TRIANGLELIST,
        3,
        true,
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
    )?;

    // Content blur bleeds ~3*radius past the box; composite over a
    // dilated rect.
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
        [full_width as f32, full_height as f32],
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
    // Composite the blurred result into the target (preserving its
    // contents).
    dx_blur_pass(
        draw,
        frame,
        composite_vertex,
        composite_fragment,
        &draw.pipelines.blur_blend_composite,
        target_rtv,
        &ping_srv,
        composite_uniforms,
        &full_vp,
        D3D_PRIMITIVE_TOPOLOGY_TRIANGLESTRIP,
        4,
        false,
    )?;
    Ok(())
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
    )
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
