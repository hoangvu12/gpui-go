//! Minimal vendored copy of the pinned `shaders/interface.rs`: only the
//! pipeline table and binding constants the build script needs to lower
//! the vendored pipelines to HLSL/DXBC. The `BufferData`/`StorageAbi`/
//! `render_layout!` halves of the pinned file mirror the runtime crate's
//! Rust types and are not needed here (the native crate's `draw.rs` owns
//! its own uniform structs).

#[derive(Clone, Copy, Debug, Eq, Hash, PartialEq)]
pub enum DataLayout {
    Instances,
    TexturedInstances,
    MonochromeSprites,
    SubpixelSprites,
    NativeOnly,
    Surface,
    Blur,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum VertexCount {
    Rectangle,
    FullscreenTriangle,
    Dynamic,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum PrimitiveTopology {
    TriangleList,
    TriangleStrip,
}

impl VertexCount {
    pub const fn fixed(self) -> Option<u32> {
        match self {
            Self::Rectangle => Some(RECTANGLE_VERTEX_COUNT),
            Self::FullscreenTriangle => Some(FULLSCREEN_TRIANGLE_VERTEX_COUNT),
            Self::Dynamic => None,
        }
    }
}

#[derive(Clone, Copy)]
pub struct Pipeline {
    pub label: &'static str,
    pub vertex_entry: &'static str,
    pub fragment_entry: &'static str,
    pub topology: PrimitiveTopology,
    pub data_layout: DataLayout,
    pub vertex_count: VertexCount,
}

macro_rules! define_pipelines {
        ($($name:ident: $label:literal, $vertex:ident, $fragment:ident, $topology:ident, $layout:ident, $vertices:ident;)*) => {
            $(
                pub const $name: Pipeline = Pipeline {
                    label: $label,
                    vertex_entry: stringify!($vertex),
                    fragment_entry: stringify!($fragment),
                    topology: PrimitiveTopology::$topology,
                    data_layout: DataLayout::$layout,
                    vertex_count: VertexCount::$vertices,
                };
            )*

            pub const ALL: &[Pipeline] = &[$($name),*];
        };
    }

define_pipelines! {
    QUADS: "quads", vertex_quad, fragment_quad, TriangleStrip, Instances, Rectangle;
    SMOOTHED_QUADS: "smoothed_quads", vertex_smoothed_quad, fragment_smoothed_quad, TriangleStrip, Instances, Rectangle;
    SHADOWS: "shadows", vertex_shadow, fragment_shadow, TriangleStrip, Instances, Rectangle;
    SMOOTHED_SHADOWS: "smoothed_shadows", vertex_smoothed_shadow, fragment_smoothed_shadow, TriangleStrip, Instances, Rectangle;
    BLUR_COMPOSITE: "blur_composite", vertex_blur_composite, fragment_blur_composite, TriangleStrip, Blur, Rectangle;
    SMOOTHED_BLUR_COMPOSITE: "smoothed_blur_composite", vertex_smoothed_blur_composite, fragment_smoothed_blur_composite, TriangleStrip, Blur, Rectangle;
    // rev 16 (ticket 51): the transformed-group composite - the quad
    // under the effect group's 2x3 matrix (local-space subtree).
    TRANSFORMED_BLUR_COMPOSITE: "transformed_blur_composite", vertex_transformed_blur_composite, fragment_transformed_blur_composite, TriangleStrip, Blur, Rectangle;
    // rev 16 (ticket 52): the blend composite - the W3C blend formula
    // against the backdrop copy (no fixed blending; the transformed
    // composite's vertex shape, identity for axis-aligned groups).
    BLEND_COMPOSITE: "blend_composite", vertex_blend_composite, fragment_blend_composite, TriangleStrip, Blur, Rectangle;
    // rev 16 (ticket 52): the drop-shadow composite - the blurred
    // silhouette tinted and drawn beneath the content quad (the plain
    // composite vertex, premultiplied blending).
    DROP_SHADOW_COMPOSITE: "drop_shadow_composite", vertex_blur_composite, fragment_drop_shadow_composite, TriangleStrip, Blur, Rectangle;
    BACKDROP_CROP: "backdrop_crop", vertex_backdrop_crop, fragment_backdrop_crop, TriangleList, Blur, FullscreenTriangle;
}

pub const GLOBAL_BIND_GROUP: u32 = 0;
pub const DATA_BIND_GROUP: u32 = 1;
pub const GLOBAL_UNIFORMS_BINDING: u32 = 0;
pub const FONT_RASTERIZATION_BINDING: u32 = 1;
pub const DATA_BUFFER_BINDING: u32 = 0;
pub const PRIMARY_TEXTURE_BINDING: u32 = 1;
pub const SECONDARY_TEXTURE_BINDING: u32 = 2;
pub const PRIMARY_SAMPLER_BINDING: u32 = 2;
pub const SURFACE_SAMPLER_BINDING: u32 = 3;
pub const RECTANGLE_VERTEX_COUNT: u32 = 4;
pub const FULLSCREEN_TRIANGLE_VERTEX_COUNT: u32 = 3;
/// D3D11 constant-buffer register of the per-draw instance base for instanced pipelines.
/// Group-0 cbuffers occupy b0 and b1, and the group-1 uniform lands on b2.
pub const DX11_DRAW_CONSTANTS_REGISTER: u32 = 3;
