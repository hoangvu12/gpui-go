#[wgsl_rs::wgsl]
mod source {
    use super::super::corner_smoothing::*;
    use wgsl_rs::std::*;

    // Defined here as a literal so the Rust-to-WGSL translator emits it, not `std`.
    #[allow(clippy::approx_constant)]
    pub const PI: f32 = 3.141592653589793;
    pub const HALF_TURN_DEGREES: f32 = 180.0;
    pub const FULL_TURN_DEGREES: f32 = 360.0;
    pub const CSS_GRADIENT_OFFSET_DEGREES: f32 = 90.0;
    // rev 16 (browser-core ticket 52): the conic gradient's full turn
    // in radians (one complete conic period's sweep).
    pub const FULL_TURN_RADIANS: f32 = 6.283185307179586;
    pub const PIXEL_ANTIALIAS_RADIUS: f32 = 0.5;
    pub const GAUSSIAN_CUTOFF_STANDARD_DEVIATIONS: f32 = 3.0;
    pub const PATTERN_COMPONENT_SCALE: f32 = 255.0;
    pub const PATTERN_PACKING_RADIX: f32 = 65535.0;
    pub const MIN_NORMALIZED_WEIGHT: f32 = 0.00001;
    pub const MIN_PATH_GRADIENT: f32 = 0.001;
    pub const UNDERLINE_WAVE_FREQUENCY: f32 = 2.0;
    pub const UNDERLINE_WAVE_HEIGHT_RATIO: f32 = 0.8;
    pub const GRADIENT_DITHER_SCALE: f32 = 0.6180339887;
    pub const GRADIENT_DITHER_RGB_STRENGTH: f32 = 2.0 / PATTERN_COMPONENT_SCALE;
    pub const GRADIENT_DITHER_ALPHA_STRENGTH: f32 = 3.0 / PATTERN_COMPONENT_SCALE;
    pub const GRADIENT_DITHER_SEED_A: Vec2f = vec2f(12.9898, 78.233);
    pub const GRADIENT_DITHER_SEED_B: Vec2f = vec2f(39.3460, 11.135);
    pub const GRADIENT_DITHER_MULTIPLIER_A: f32 = 43758.5453;
    pub const GRADIENT_DITHER_MULTIPLIER_B: f32 = 24634.6345;
    pub const DIAGONAL_STRIPE_ANGLE: f32 = PI / 4.0;
    pub const REC_601_LUMA_WEIGHTS: Vec3f = vec3f(0.30, 0.59, 0.11);
    pub const LINEAR_RGB_LUMA_WEIGHTS: Vec3f = vec3f(0.2126, 0.7152, 0.0722);
    pub const SRGB_DECODE_CUTOFF: f32 = 0.04045;
    pub const SRGB_ENCODE_CUTOFF: f32 = 0.0031308;
    pub const SRGB_TRANSFER_OFFSET: f32 = 0.055;
    pub const SRGB_TRANSFER_SCALE: f32 = 1.055;
    pub const SRGB_DECODE_EXPONENT: f32 = 2.4;
    pub const SRGB_LINEAR_SCALE: f32 = 12.92;
    pub const ERROR_FUNCTION_LINEAR_COEFFICIENT: f32 = 0.278393;
    pub const ERROR_FUNCTION_QUADRATIC_COEFFICIENT: f32 = 0.230389;
    pub const ERROR_FUNCTION_CUBIC_COEFFICIENT: f32 = 0.000972;
    pub const ERROR_FUNCTION_QUARTIC_COEFFICIENT: f32 = 0.078108;
    pub const DARK_TEXT_CONTRAST_SCALE: f32 = 4.0;
    pub const DARK_TEXT_BRIGHTNESS_CUTOFF: f32 = 0.75;
    pub const LINEAR_SRGB_TO_CONE_RESPONSE: Mat3x3f = mat3x3f(
        vec3f(0.4122214708, 0.2119034982, 0.0883024619),
        vec3f(0.5363325363, 0.6806995451, 0.2817188376),
        vec3f(0.0514459929, 0.1073969566, 0.6299787005),
    );
    pub const CONE_RESPONSE_TO_OKLAB: Mat3x3f = mat3x3f(
        vec3f(0.2104542553, 1.9779984951, 0.0259040371),
        vec3f(0.7936177850, -2.4285922050, 0.7827717662),
        vec3f(-0.0040720468, 0.4505938995, -0.8086757660),
    );
    pub const OKLAB_TO_CONE_RESPONSE: Mat3x3f = mat3x3f(
        vec3f(1.0, 1.0, 1.0),
        vec3f(0.3963377774, -0.1055613458, -0.0894841775),
        vec3f(0.2158037573, -0.0638541728, -1.2914855480),
    );
    pub const CONE_RESPONSE_TO_LINEAR_SRGB: Mat3x3f = mat3x3f(
        vec3f(4.0767416621, -1.2684380046, -0.0041960863),
        vec3f(-3.3077115913, 2.6097574011, -0.7034186147),
        vec3f(0.2309699292, -0.3413193965, 1.7076147010),
    );

    #[repr(u32)]
    #[derive(Clone, Copy, Debug, PartialEq, Eq, Wgsl)]
    pub enum ShaderBool {
        Disabled = 0,
        Enabled = 1,
    }

    #[repr(u32)]
    #[derive(Clone, Copy, Debug, PartialEq, Eq, Wgsl)]
    pub enum BackgroundTag {
        Solid = 0,
        LinearGradient = 1,
        PatternSlash = 2,
        Checkerboard = 3,
        // rev 16 (browser-core ticket 52): the R16 gradient-table arm -
        // the background references a scene-level gradient entry by
        // index (the index stored in the record's
        // `gradient_angle_or_pattern_height` field as f32 bits; the
        // geometry and baked ramp row live in the table).
        GradientTable = 4,
    }

    #[repr(u32)]
    #[derive(Clone, Copy, Debug, PartialEq, Eq, Wgsl)]
    pub enum ColorSpace {
        Srgb = 0,
        Oklab = 1,
    }

    #[repr(u32)]
    #[derive(Clone, Copy, Debug, PartialEq, Eq, Wgsl)]
    pub enum BorderStyle {
        Solid = 0,
        Dashed = 1,
    }

    #[repr(u32)]
    #[derive(Clone, Copy, Debug, PartialEq, Eq, Wgsl)]
    pub enum SurfaceColorFormat {
        Rgba = 0,
        Yuv = 1,
    }

    #[repr(u32)]
    #[derive(Clone, Copy, Debug, PartialEq, Eq, Wgsl)]
    pub enum BlurCompositeClip {
        None = 0,
        RoundedBounds = 1,
    }

    #[repr(u32)]
    #[derive(Clone, Copy, Debug, PartialEq, Eq, Wgsl)]
    pub enum DownsampleMode {
        Copy = 0,
        HalfResolution = 1,
    }

    #[repr(C)]
    #[derive(Clone, Copy, PartialEq, Wgsl)]
    pub struct GlobalUniforms {
        pub viewport_size: Vec2f,
        pub premultiplied_alpha: ShaderBool,
        pub padding: u32,
    }

    #[repr(C)]
    #[derive(Clone, Copy, PartialEq, Wgsl)]
    pub struct FontRasterizationUniforms {
        pub gamma_ratios: Vec4f,
        pub grayscale_enhanced_contrast: f32,
        pub subpixel_enhanced_contrast: f32,
        pub uses_blue_green_red_subpixel_order: ShaderBool,
        pub padding: u32,
    }

    uniform!(group(0), binding(0), GLOBALS: GlobalUniforms);
    uniform!(group(0), binding(1), FONT_RASTERIZATION: FontRasterizationUniforms);

    // rev 16 (browser-core ticket 52): the scene-level gradient table
    // and its baked ramp texture, shared by every vendored module that
    // includes this common block (the quad and shadow pipelines'
    // gradient-table backgrounds, and the composite pass's gradient
    // MASKS). The bindings sit above every module's own group-1 slots
    // (quads/shadows own binding 0, the blur module owns 0-4, the
    // surface module 0-3) so no two declarations collide. Registers:
    // the storage buffer at t7, the ramp texture at t8, the sampler at
    // s9 (binding + 2, the shared group-1 offset).
    //
    // The instance struct is the ABI record's payload RESHAPED for the
    // HLSL structured-buffer layout (the vec4-bearing fields land on
    // 16-byte-aligned offsets, and the struct's size rounds up to its
    // 16-byte alignment - a 64-byte stride): the draw path converts
    // each `GpuiGoSceneGradientRecord` (56 bytes, geometry at word 3)
    // into this 64-byte form.
    #[derive(Clone, Copy, Wgsl)]
    pub struct GradientInstance {
        pub kind: u32,
        pub tile: u32,
        pub color_space: u32,
        pub stop_count: u32,
        // linear: (x0, y0, x1, y1); radial: (cx, cy, r0, r1);
        // conic: (cx, cy, start_angle, rotation).
        pub geometry0: Vec4f,
        // radial: (aspect, 0, 0, 0); the rest zero.
        pub geometry1: Vec4f,
        pub stop_row: u32,
        pub record_size: u32,
        pub padding0: u32,
        pub padding1: u32,
    }
    storage!(group(1), binding(5), GRADIENTS: RuntimeArray<GradientInstance>);
    texture!(group(1), binding(6), GRADIENT_RAMP: Texture2D<f32>);
    sampler!(group(1), binding(7), GRADIENT_SAMPLER: Sampler);

    #[repr(C)]
    #[derive(Clone, Copy, Wgsl)]
    pub struct Bounds {
        pub origin: Vec2f,
        pub size: Vec2f,
    }

    // rev 16 (browser-core ticket 50): the mask's fade edges in the
    // ABI's word order (left, top, right, bottom) - the record is the
    // instance layout, so the struct mirrors it exactly.
    #[repr(C)]
    #[derive(Clone, Copy, Wgsl)]
    pub struct MaskFade {
        pub left: f32,
        pub top: f32,
        pub right: f32,
        pub bottom: f32,
    }

    // rev 16 (browser-core ticket 50): the extended content mask - the
    // pin's bounds + per-edge fade ramp, plus the rounded-clip corner
    // radii and smoothing. This struct IS the ABI record
    // `GpuiGoSceneContentMask` (52 bytes): the vendored quad/shadow
    // pipelines' instance layout mirrors the ABI record field for
    // field, so the draw path uploads the records directly.
    #[repr(C)]
    #[derive(Clone, Copy, Wgsl)]
    pub struct ContentMask {
        pub bounds: Bounds,
        pub fade_out: MaskFade,
        pub corner_radii: Corners,
        pub corner_smoothing: f32,
    }

    impl ContentMask {
        /// The mask's coverage at the given window-space position: 1
        /// inside, 0 outside, a linear ramp to 0 across each edge's
        /// fade distance, and - rev 16 - the rounded-corner clip when
        /// the mask carries radii (the rounded-rectangle SDF the quad
        /// pipeline already computes; the INNERMOST radii apply inline,
        /// ancestors' rounded clips become effect groups).
        pub fn alpha(content_mask: ContentMask, position: Vec2f) -> f32 {
            let bounds = content_mask.bounds;
            let fade = content_mask.fade_out;
            let mut alpha = 1.0;
            if fade.left > 0.0 {
                alpha *= saturate((position.x - bounds.origin.x) / fade.left);
            }
            if fade.right > 0.0 {
                alpha *= saturate((bounds.origin.x + bounds.size.x - position.x) / fade.right);
            }
            if fade.top > 0.0 {
                alpha *= saturate((position.y - bounds.origin.y) / fade.top);
            }
            if fade.bottom > 0.0 {
                alpha *= saturate((bounds.origin.y + bounds.size.y - position.y) / fade.bottom);
            }
            if !Corners::is_zero(content_mask.corner_radii) {
                // rev 16, ticket 52 (closing 50's handed-off gap): the
                // smoothed-corner SDF when the mask carries smoothing
                // (the prepared corner layout computed per fragment -
                // the same math the smoothed pipelines run in the
                // vertex); the plain rrect SDF otherwise, bit-exact.
                let signed_distance = select(
                    rounded_rectangle_signed_distance(
                        position,
                        bounds,
                        content_mask.corner_radii,
                    ),
                    prepared_corner_signed_distance(
                        position,
                        bounds,
                        content_mask.corner_radii,
                        content_mask.corner_smoothing,
                        prepare_corners(
                            bounds.size,
                            content_mask.corner_radii,
                            content_mask.corner_smoothing,
                            true,
                        ),
                    ),
                    content_mask.corner_smoothing > 0.0,
                );
                alpha *= antialiased_coverage(signed_distance);
            }
            alpha
        }
    }

    impl Bounds {
        pub fn position(bounds: Bounds, unit_position: Vec2f) -> Vec2f {
            bounds.origin + unit_position * bounds.size
        }

        pub fn half_size(bounds: Bounds) -> Vec2f {
            bounds.size / 2.0
        }

        pub fn center(bounds: Bounds) -> Vec2f {
            bounds.origin + Bounds::half_size(bounds)
        }
    }

    #[repr(C)]
    #[derive(Clone, Copy, Wgsl)]
    pub struct Corners {
        pub top_left: f32,
        pub top_right: f32,
        pub bottom_right: f32,
        pub bottom_left: f32,
    }

    impl Corners {
        pub fn is_zero(corners: Corners) -> bool {
            corners.top_left == 0.0
                && corners.top_right == 0.0
                && corners.bottom_right == 0.0
                && corners.bottom_left == 0.0
        }
    }

    #[derive(Clone, Copy, Wgsl)]
    pub struct Edges {
        pub top: f32,
        pub right: f32,
        pub bottom: f32,
        pub left: f32,
    }

    impl Edges {
        pub fn is_zero(edges: Edges) -> bool {
            edges.top == 0.0 && edges.right == 0.0 && edges.bottom == 0.0 && edges.left == 0.0
        }
    }
    #[derive(Clone, Copy, Wgsl)]
    pub struct Hsla {
        pub h: f32,
        pub s: f32,
        pub l: f32,
        pub a: f32,
    }
    #[derive(Clone, Copy, Wgsl)]
    pub struct LinearColorStop {
        pub color: Hsla,
        pub percentage: f32,
    }
    #[derive(Clone, Copy, Wgsl)]
    pub struct Background {
        pub tag: BackgroundTag,
        pub color_space: ColorSpace,
        pub solid: Hsla,
        pub gradient_angle_or_pattern_height: f32,
        pub colors: [LinearColorStop; 2],
        pub padding: u32,
    }
    #[derive(Clone, Copy, Wgsl)]
    pub struct AtlasTextureId {
        pub index: u32,
        pub kind: u32,
    }
    #[derive(Clone, Copy, Wgsl)]
    pub struct AtlasBounds {
        pub origin: Vec2i,
        pub size: Vec2i,
    }
    #[derive(Clone, Copy, Wgsl)]
    pub struct AtlasTile {
        pub texture_id: AtlasTextureId,
        pub tile_id: u32,
        pub padding: u32,
        pub bounds: AtlasBounds,
    }
    #[derive(Clone, Copy, Wgsl)]
    pub struct TransformationMatrix {
        pub rotation_scale: Mat2x2f,
        pub translation: Vec2f,
    }

    impl TransformationMatrix {
        pub fn transform_position(transform: TransformationMatrix, position: Vec2f) -> Vec2f {
            transpose(transform.rotation_scale) * position + transform.translation
        }
    }

    #[derive(Clone, Copy, Wgsl)]
    pub struct RectangleVertex {
        pub unit_position: Vec2f,
        pub viewport_position: Vec2f,
        pub clip_position: Vec4f,
    }

    #[derive(Clone, Copy, Wgsl)]
    pub struct FullscreenVertex {
        pub texture_coordinates: Vec2f,
        pub clip_position: Vec4f,
    }

    pub fn is_enabled(value: ShaderBool) -> bool {
        value == ShaderBool::Enabled
    }

    pub fn transparent() -> Vec4f {
        vec4f(0.0, 0.0, 0.0, 0.0)
    }

    pub fn antialiased_coverage(signed_distance: f32) -> f32 {
        saturate(PIXEL_ANTIALIAS_RADIUS - signed_distance)
    }

    pub fn premultiply(color: Vec4f, coverage: f32) -> Vec4f {
        let alpha = color.w * coverage;
        vec4f(color.x * alpha, color.y * alpha, color.z * alpha, alpha)
    }

    pub fn atlas_texture_coordinates(
        unit_position: Vec2f,
        tile: AtlasTile,
        atlas_size: Vec2u,
    ) -> Vec2f {
        let tile_origin = vec2f(tile.bounds.origin.x as f32, tile.bounds.origin.y as f32);
        let tile_size = vec2f(tile.bounds.size.x as f32, tile.bounds.size.y as f32);
        let texture_size = vec2f(atlas_size.x as f32, atlas_size.y as f32);
        (tile_origin + unit_position * tile_size) / texture_size
    }

    pub fn color_brightness(color: Vec3f) -> f32 {
        dot(color, REC_601_LUMA_WEIGHTS)
    }
    pub fn light_on_dark_contrast(enhanced_contrast: f32, color: Vec3f) -> f32 {
        let darkness = saturate(
            DARK_TEXT_CONTRAST_SCALE * (DARK_TEXT_BRIGHTNESS_CUTOFF - color_brightness(color)),
        );
        enhanced_contrast * darkness
    }

    pub fn enhance_contrast<T>(alpha: T, contrast: f32) -> T
    where
        T: Copy
            + std::ops::Mul<f32, Output = T>
            + std::ops::Add<f32, Output = T>
            + std::ops::Div<T, Output = T>,
    {
        alpha * (contrast + 1.0) / (alpha * contrast + 1.0)
    }

    pub fn apply_alpha_correction<T>(alpha: T, brightness: T, gamma_ratios: Vec4f) -> T
    where
        T: Copy
            + std::ops::Mul<f32, Output = T>
            + std::ops::Mul<T, Output = T>
            + std::ops::Add<f32, Output = T>
            + std::ops::Add<T, Output = T>,
        f32: std::ops::Sub<T, Output = T>,
    {
        let brightness_adjustment = brightness * gamma_ratios.x + gamma_ratios.y;
        let correction =
            brightness_adjustment * alpha + (brightness * gamma_ratios.z + gamma_ratios.w);
        alpha + alpha * (1.0 - alpha) * correction
    }
    pub fn apply_contrast_and_gamma_correction(
        sample: f32,
        color: Vec3f,
        enhanced_contrast_factor: f32,
        gamma_ratios: Vec4f,
    ) -> f32 {
        let enhanced_contrast = light_on_dark_contrast(enhanced_contrast_factor, color);
        apply_alpha_correction::<f32>(
            enhance_contrast::<f32>(sample, enhanced_contrast),
            color_brightness(color),
            gamma_ratios,
        )
    }
    pub fn apply_contrast_and_gamma_correction3(
        sample: Vec3f,
        color: Vec3f,
        enhanced_contrast_factor: f32,
        gamma_ratios: Vec4f,
    ) -> Vec3f {
        let contrasted = enhance_contrast::<Vec3f>(
            sample,
            light_on_dark_contrast(enhanced_contrast_factor, color),
        );
        apply_alpha_correction::<Vec3f>(contrasted, color, gamma_ratios)
    }
    pub fn rectangle_corner(vertex_id: u32) -> Vec2f {
        vec2f((vertex_id & 1u32) as f32, 0.5 * (vertex_id & 2u32) as f32)
    }

    pub fn viewport_to_clip_position(position: Vec2f) -> Vec4f {
        let clip_position =
            position / get!(GLOBALS).viewport_size * vec2f(2.0, -2.0) + vec2f(-1.0, 1.0);
        vec4f(clip_position.x, clip_position.y, 0.0, 1.0)
    }

    pub fn rectangle_vertex(vertex_id: u32, bounds: Bounds) -> RectangleVertex {
        let unit_position = rectangle_corner(vertex_id);
        let viewport_position = Bounds::position(bounds, unit_position);
        RectangleVertex {
            unit_position,
            viewport_position,
            clip_position: viewport_to_clip_position(viewport_position),
        }
    }

    pub fn transformed_rectangle_vertex(
        vertex_id: u32,
        bounds: Bounds,
        transform: TransformationMatrix,
    ) -> RectangleVertex {
        let unit_position = rectangle_corner(vertex_id);
        let viewport_position = TransformationMatrix::transform_position(
            transform,
            Bounds::position(bounds, unit_position),
        );
        RectangleVertex {
            unit_position,
            viewport_position,
            clip_position: viewport_to_clip_position(viewport_position),
        }
    }

    pub fn fullscreen_vertex(vertex_id: u32) -> FullscreenVertex {
        let texture_coordinates = vec2f(
            ((vertex_id << 1u32) & 2u32) as f32,
            (vertex_id & 2u32) as f32,
        );
        FullscreenVertex {
            texture_coordinates,
            clip_position: vec4f(
                texture_coordinates.x * 2.0 - 1.0,
                1.0 - texture_coordinates.y * 2.0,
                0.0,
                1.0,
            ),
        }
    }

    pub fn clip_distances(position: Vec2f, clip_bounds: Bounds) -> Vec4f {
        let distance_from_top_left = position - clip_bounds.origin;
        let distance_from_bottom_right = clip_bounds.origin + clip_bounds.size - position;
        vec4f(
            distance_from_top_left.x,
            distance_from_bottom_right.x,
            distance_from_top_left.y,
            distance_from_bottom_right.y,
        )
    }

    pub fn is_clipped(distances: Vec4f) -> bool {
        min(min(distances.x, distances.y), min(distances.z, distances.w)) < 0.0
    }

    pub fn unclipped_distances() -> Vec4f {
        vec4f(1.0, 1.0, 1.0, 1.0)
    }
    pub fn srgb_to_linear(srgb: Vec3f) -> Vec3f {
        let cutoff = vec3b(
            srgb.x < SRGB_DECODE_CUTOFF,
            srgb.y < SRGB_DECODE_CUTOFF,
            srgb.z < SRGB_DECODE_CUTOFF,
        );
        let higher = pow(
            (srgb + SRGB_TRANSFER_OFFSET) / SRGB_TRANSFER_SCALE,
            vec3f(
                SRGB_DECODE_EXPONENT,
                SRGB_DECODE_EXPONENT,
                SRGB_DECODE_EXPONENT,
            ),
        );
        select(higher, srgb / SRGB_LINEAR_SCALE, cutoff)
    }
    pub fn linear_to_srgb(linear: Vec3f) -> Vec3f {
        let cutoff = vec3b(
            linear.x < SRGB_ENCODE_CUTOFF,
            linear.y < SRGB_ENCODE_CUTOFF,
            linear.z < SRGB_ENCODE_CUTOFF,
        );
        let inverse_exponent = 1.0 / SRGB_DECODE_EXPONENT;
        let higher = SRGB_TRANSFER_SCALE
            * pow(
                linear,
                vec3f(inverse_exponent, inverse_exponent, inverse_exponent),
            )
            - SRGB_TRANSFER_OFFSET;
        select(higher, linear * SRGB_LINEAR_SCALE, cutoff)
    }
    pub fn linear_to_srgba(color: Vec4f) -> Vec4f {
        let red_green_blue = linear_to_srgb(color.rgb());
        vec4f(
            red_green_blue.x,
            red_green_blue.y,
            red_green_blue.z,
            color.w,
        )
    }
    pub fn srgba_to_linear(color: Vec4f) -> Vec4f {
        let red_green_blue = srgb_to_linear(color.rgb());
        vec4f(
            red_green_blue.x,
            red_green_blue.y,
            red_green_blue.z,
            color.w,
        )
    }
    pub fn hue_to_unit_rgb(hue: f32) -> Vec3f {
        let phases = fract(vec3f(hue, hue + 2.0 / 3.0, hue + 1.0 / 3.0));
        saturate(abs(phases * 6.0 - 3.0) - 1.0)
    }

    pub fn hsla_to_rgba(hsla: Hsla) -> Vec4f {
        let chroma = hsla.s * (1.0 - abs(2.0 * hsla.l - 1.0));
        let red_green_blue = (hue_to_unit_rgb(hsla.h) - 0.5) * chroma + hsla.l;
        vec4f(red_green_blue.x, red_green_blue.y, red_green_blue.z, hsla.a)
    }
    pub fn linear_srgb_to_oklab(color: Vec4f) -> Vec4f {
        // Keeps the retired Metal shader's 2.2 transfer curve for pixel parity.
        let linear_srgb = pow(color.rgb(), vec3f(2.2, 2.2, 2.2));
        let cone_response = LINEAR_SRGB_TO_CONE_RESPONSE * linear_srgb;
        let cube_root = vec3f(1.0 / 3.0, 1.0 / 3.0, 1.0 / 3.0);
        let oklab = CONE_RESPONSE_TO_OKLAB * pow(cone_response, cube_root);
        vec4f(oklab.x, oklab.y, oklab.z, color.w)
    }
    pub fn oklab_to_linear_srgb(color: Vec4f) -> Vec4f {
        let cone_root = OKLAB_TO_CONE_RESPONSE * color.rgb();
        let cone_response = cone_root * cone_root * cone_root;
        let linear_srgb = CONE_RESPONSE_TO_LINEAR_SRGB * cone_response;
        let srgb = pow(linear_srgb, vec3f(1.0 / 2.2, 1.0 / 2.2, 1.0 / 2.2));
        vec4f(srgb.x, srgb.y, srgb.z, color.w)
    }
    pub fn over(below: Vec4f, above: Vec4f) -> Vec4f {
        let alpha = above.w + below.w * (1.0 - above.w);
        if alpha == 0.0 {
            return transparent();
        }
        let red_green_blue =
            (above.rgb() * above.w + below.rgb() * below.w * (1.0 - above.w)) / alpha;
        vec4f(red_green_blue.x, red_green_blue.y, red_green_blue.z, alpha)
    }
    pub fn gaussian(position: f32, standard_deviation: f32) -> f32 {
        let variance = standard_deviation * standard_deviation;
        exp(-(position * position) / (2.0 * variance)) / (sqrt(2.0 * PI) * standard_deviation)
    }
    pub fn approximate_error_function(value: Vec2f) -> Vec2f {
        let signs = sign(value);
        let absolute_value = abs(value);
        let polynomial = 1.0
            + absolute_value
                * (ERROR_FUNCTION_LINEAR_COEFFICIENT
                    + absolute_value
                        * (ERROR_FUNCTION_QUADRATIC_COEFFICIENT
                            + absolute_value
                                * (ERROR_FUNCTION_CUBIC_COEFFICIENT
                                    + absolute_value * ERROR_FUNCTION_QUARTIC_COEFFICIENT)));
        let polynomial_squared = polynomial * polynomial;
        signs - signs / (polynomial_squared * polynomial_squared)
    }
    pub fn integrated_rounded_rectangle_coverage(
        horizontal_position: f32,
        vertical_position: f32,
        standard_deviation: f32,
        corner_radius: f32,
        half_size: Vec2f,
    ) -> f32 {
        let distance_beyond_corner = min(half_size.y - corner_radius - abs(vertical_position), 0.0);
        let curved_extent = half_size.x - corner_radius
            + sqrt(max(
                0.0,
                corner_radius * corner_radius - distance_beyond_corner * distance_beyond_corner,
            ));
        let integration_bounds = horizontal_position + vec2f(-curved_extent, curved_extent);
        let normalized_bounds = integration_bounds * (sqrt(0.5) / standard_deviation);
        let integral = 0.5 + 0.5 * approximate_error_function(normalized_bounds);
        integral.y - integral.x
    }
    pub fn pick_corner_radius(center_to_point: Vec2f, radii: Corners) -> f32 {
        let left = select(radii.bottom_left, radii.top_left, center_to_point.y < 0.0);
        let right = select(radii.bottom_right, radii.top_right, center_to_point.y < 0.0);
        select(right, left, center_to_point.x < 0.0)
    }
    pub fn rounded_rectangle_signed_distance_from_corner(
        corner_center_to_point: Vec2f,
        corner_radius: f32,
    ) -> f32 {
        if corner_radius == 0.0 {
            max(corner_center_to_point.x, corner_center_to_point.y)
        } else {
            length(max(vec2f(0.0, 0.0), corner_center_to_point))
                + min(0.0, max(corner_center_to_point.x, corner_center_to_point.y))
                - corner_radius
        }
    }
    pub fn rounded_rectangle_signed_distance(
        point: Vec2f,
        bounds: Bounds,
        corner_radii: Corners,
    ) -> f32 {
        let half_size = Bounds::half_size(bounds);
        let center_to_point = point - Bounds::center(bounds);
        let corner_radius = pick_corner_radius(center_to_point, corner_radii);
        rounded_rectangle_signed_distance_from_corner(
            abs(center_to_point) - half_size + corner_radius,
            corner_radius,
        )
    }

    pub fn gaussian_signed_distance_coverage(distance: f32, standard_deviation: f32) -> f32 {
        let normalized = distance / (sqrt(2.0) * standard_deviation);
        saturate(0.5 - 0.5 * approximate_error_function(vec2f(normalized, normalized)).x)
    }

    pub fn blend_color(color: Vec4f, alpha_factor: f32) -> Vec4f {
        let alpha = color.w * alpha_factor;
        let multiplier = select(1.0, alpha, is_enabled(get!(GLOBALS).premultiplied_alpha));
        vec4f(
            color.x * multiplier,
            color.y * multiplier,
            color.z * multiplier,
            alpha,
        )
    }

    #[derive(Clone, Copy, Wgsl)]
    pub struct Paint {
        pub background: Background,
        pub bounds: Bounds,
    }

    impl Paint {
        pub fn new(background: Background, bounds: Bounds) -> Paint {
            Paint { background, bounds }
        }
    }

    #[derive(Clone, Copy, Wgsl)]
    pub struct PreparedPaint {
        pub solid: Vec4f,
        pub color0: Vec4f,
        pub color1: Vec4f,
    }

    impl PreparedPaint {
        pub fn new(solid: Vec4f, color0: Vec4f, color1: Vec4f) -> PreparedPaint {
            PreparedPaint {
                solid,
                color0,
                color1,
            }
        }
    }

    pub fn prepare_paint(paint: Paint) -> PreparedPaint {
        let mut prepared = PreparedPaint::new(transparent(), transparent(), transparent());

        if paint.background.tag == BackgroundTag::LinearGradient {
            prepared.color0 = hsla_to_rgba(paint.background.colors[0usize].color);
            prepared.color1 = hsla_to_rgba(paint.background.colors[1usize].color);
            if paint.background.color_space == ColorSpace::Oklab {
                prepared.color0 = linear_srgb_to_oklab(prepared.color0);
                prepared.color1 = linear_srgb_to_oklab(prepared.color1);
            }
            // rev-15 divergence toward CSS: in the sRGB space the stops
            // stay in the plain sRGB-encoded space the interpolation
            // runs in (the pinned path re-encodes them once more, which
            // only made sense together with the removed final decode).
        } else if paint.background.tag == BackgroundTag::GradientTable {
            // rev 16 (browser-core ticket 52): the table gradient is
            // evaluated PER PIXEL in the fragment (the t parameter and
            // the ramp sample vary across the primitive); the vertex's
            // prepared colors stay transparent and the fragment's
            // `gradient_table_color` arm owns the color.
            prepared = PreparedPaint::new(transparent(), transparent(), transparent());
        } else {
            prepared.solid = hsla_to_rgba(paint.background.solid);
        }

        prepared
    }

    // rev 16 (browser-core ticket 52): the gradient table's parameter
    // and ramp sample. The t computation follows the record's kind:
    //   linear - the projection onto the [x0, y0] -> [x1, y1] line
    //     (the CSS gradient line's endpoints, resolved by the caller
    //     per Chromium's EndPointsFromAngle);
    //   radial - the ellipse-normalized radius (the position scaled by
    //     1/aspect on x, in ry units, minus r0 over the r0->r1 delta);
    //   conic - the clockwise-from-up angle from the center, swept
    //     from `start_angle` over `rotation` radians (CSS conic's
    //     0deg-up clockwise convention).
    // The TILE mode wraps t before the ramp is sampled: pad clamps at
    // the row's own ends (the bake padded them), repeat applies
    // fract (CSS repeating-*), reflect mirrors (a triangle wave).
    pub fn gradient_table_parameter(kind: u32, geometry0: Vec4f, geometry1: Vec4f, position: Vec2f) -> f32 {
        if kind == 1u32 {
            // radial
            let center = geometry0.xy();
            let r0 = geometry0.z;
            let r1 = geometry0.w;
            let aspect = geometry1.x;
            let scaled = vec2f((position.x - center.x) / aspect, position.y - center.y);
            return (length(scaled) - r0) / (r1 - r0);
        } else if kind == 2u32 {
            // conic: 0 is up, clockwise positive (screen y grows down).
            let center = geometry0.xy();
            let start_angle = geometry0.z;
            let rotation = geometry0.w;
            let delta = position - center;
            let angle = atan2(delta.x, -delta.y);
            let swept = signed_modulo(angle - start_angle, FULL_TURN_RADIANS * 2.0);
            return swept / rotation;
        }
        // linear
        let start = geometry0.xy();
        let end = geometry0.zw();
        let line = end - start;
        return dot(position - start, line) / dot(line, line);
    }

    // The ramp row's sampling coordinate for a gradient parameter
    // under a tile mode: texel i of the 1024-wide row holds t = i/1023,
    // so u = (t*1023 + 0.5)/1024 puts t=0 at texel 0 and t=1 at texel
    // 1023 with the hardware's linear filtering interpolating between
    // the baked bytes (CSS's linear interpolation rule, the design
    // §3.8's ramp-row decision).
    pub fn gradient_ramp_coordinate(t: f32) -> f32 {
        (t * 1023.0 + 0.5) / 1024.0
    }

    // rev 16 (browser-core ticket 52): the gradient-table background's
    // color at a position: read the referenced table entry, compute
    // the kind's t, wrap it under the tile mode, sample the baked ramp
    // row (premultiplied sRGB) and un-premultiply for the pipeline's
    // straight-color contract. No dither: the 1024-texel ramp's
    // quantization is far below the R15 two-stop path's banding (the
    // dither's reason), and the spec tests' CPU references compare
    // the exact filtered bytes.
    pub fn gradient_table_color(paint: Paint, position: Vec2f) -> Vec4f {
        let index = u32(paint.background.gradient_angle_or_pattern_height);
        let gradient = get!(GRADIENTS)[index as usize];
        let t = gradient_table_parameter(gradient.kind, gradient.geometry0, gradient.geometry1, position);
        let wrapped = select(
            select(t, fract(t), gradient.tile == 1u32),
            1.0 - abs(signed_modulo(t, 2.0) - 1.0),
            gradient.tile == 2u32,
        );
        let rows = f32(texture_dimensions(GRADIENT_RAMP).y);
        let ramp = texture_sample_level(
            GRADIENT_RAMP,
            GRADIENT_SAMPLER,
            vec2f(gradient_ramp_coordinate(wrapped), (f32(gradient.stop_row) + 0.5) / rows),
            0.0,
        );
        let alpha = ramp.w;
        select(transparent(), ramp / vec4f(alpha, alpha, alpha, 1.0), alpha > 0.0)
    }

    pub fn linear_gradient_ratio(paint: Paint, position: Vec2f) -> f32 {
        // rev-15 divergence toward CSS: the gradient line passes through
        // the center at the CSS angle and its length is
        // |W·sin θ| + |H·cos θ| (css-images-3 §linear-gradients), so the
        // box's corners receive the end colors exactly; Chromium's
        // EndPointsFromAngle derives the same endpoints
        // (core/css/css_gradient_value.cc:1284-1350 @ 0d4dcd4f). The
        // pinned formula normalizes by the box's dominant axis instead,
        // which stops diagonal gradients short of the corners. 0deg
        // points up and the angle grows clockwise (the pinned angle
        // convention), so the unit direction is (sin θ, -cos θ) in
        // screen coordinates — exactly the pinned unsquashed `cos,
        // sin` pair of radians = θ - 90°.
        let radians = (paint.background.gradient_angle_or_pattern_height % FULL_TURN_DEGREES
            - CSS_GRADIENT_OFFSET_DEGREES)
            * PI
            / HALF_TURN_DEGREES;
        let direction = vec2f(cos(radians), sin(radians));
        let line_length = abs(paint.bounds.size.x * direction.x)
            + abs(paint.bounds.size.y * direction.y);
        let ratio = dot(position - Bounds::center(paint.bounds), direction) / line_length + 0.5;

        let first_stop = paint.background.colors[0usize].percentage;
        let last_stop = paint.background.colors[1usize].percentage;
        saturate((ratio - first_stop) / (last_stop - first_stop))
    }

    pub fn gradient_dither(position: Vec2f) -> Vec4f {
        let seed = position * GRADIENT_DITHER_SCALE;
        let noise_a = fract(sin(dot(seed, GRADIENT_DITHER_SEED_A)) * GRADIENT_DITHER_MULTIPLIER_A);
        let noise_b = fract(sin(dot(seed, GRADIENT_DITHER_SEED_B)) * GRADIENT_DITHER_MULTIPLIER_B);
        let triangular_noise = noise_a + noise_b - 1.0;
        vec4f(
            triangular_noise * GRADIENT_DITHER_RGB_STRENGTH,
            triangular_noise * GRADIENT_DITHER_RGB_STRENGTH,
            triangular_noise * GRADIENT_DITHER_RGB_STRENGTH,
            triangular_noise * GRADIENT_DITHER_ALPHA_STRENGTH,
        )
    }

    pub fn linear_gradient_color(paint: Paint, position: Vec2f, prepared: PreparedPaint) -> Vec4f {
        let ratio = linear_gradient_ratio(paint, position);
        let ratio4 = vec4f(ratio, ratio, ratio, ratio);
        let mut color = transparent();
        if paint.background.color_space == ColorSpace::Oklab {
            // The pinned Oklab path (unchanged): straight mix in Oklab,
            // decoded back to linear sRGB.
            let interpolated = mix(prepared.color0, prepared.color1, ratio4);
            color = oklab_to_linear_srgb(interpolated);
        } else {
            // rev-15 divergence toward CSS: premultiplied interpolation
            // in the sRGB (encoded) color space
            // (css-images-4 §coloring-gradient-line; Chromium
            // premultiplies the stops, platform/graphics/gradient.cc:287
            // @ 0d4dcd4f). The pinned path mixes the stops straight in a
            // double-encoded space and then decodes the result to linear,
            // so a gradient fading to `transparent` goes grey and the
            // output space disagrees with the solid path. The patched
            // path mixes each stop's sRGB channels premultiplied by its
            // alpha, un-premultiplies the result, and emits the plain
            // encoded straight color the solid path emits (the
            // per-primitive premultiply and the premultiplied-over blend
            // then composite it exactly like a solid quad).
            let interpolated = mix(
                prepared.color0 * vec4f(prepared.color0.w, prepared.color0.w, prepared.color0.w, 1.0),
                prepared.color1 * vec4f(prepared.color1.w, prepared.color1.w, prepared.color1.w, 1.0),
                ratio4,
            );
            let alpha = interpolated.w;
            let unpremultiplied = interpolated / vec4f(alpha, alpha, alpha, 1.0);
            color = select(transparent(), unpremultiplied, alpha > 0.0);
        }
        color + gradient_dither(position)
    }

    pub fn slash_pattern_color(paint: Paint, position: Vec2f, solid: Vec4f) -> Vec4f {
        let encoded = paint.background.gradient_angle_or_pattern_height;
        let pattern_width = (encoded / PATTERN_PACKING_RADIX) / PATTERN_COMPONENT_SCALE;
        let pattern_interval = (encoded % PATTERN_PACKING_RADIX) / PATTERN_COMPONENT_SCALE;
        let pattern_height = pattern_width + pattern_interval;
        let period = pattern_height * sin(DIAGONAL_STRIPE_ANGLE);
        let rotation = mat2x2f(
            vec2f(cos(DIAGONAL_STRIPE_ANGLE), -sin(DIAGONAL_STRIPE_ANGLE)),
            vec2f(sin(DIAGONAL_STRIPE_ANGLE), cos(DIAGONAL_STRIPE_ANGLE)),
        );
        let pattern = (rotation * (position - paint.bounds.origin)).x % period;
        let distance =
            min(pattern, period - pattern) - period * (pattern_width / pattern_height) / 2.0;
        let mut color = solid;
        color.w *= antialiased_coverage(distance);
        color
    }

    pub fn checkerboard_color(paint: Paint, position: Vec2f, solid: Vec4f) -> Vec4f {
        let relative = position - paint.bounds.origin;
        let square_size = paint.background.gradient_angle_or_pattern_height;
        let colored = (floor(relative.x / square_size) + floor(relative.y / square_size)) % 2.0;
        let mut color = solid;
        color.w *= saturate(colored);
        color
    }

    pub fn paint_color(paint: Paint, position: Vec2f, prepared: PreparedPaint) -> Vec4f {
        let mut color = prepared.solid;
        #[wgsl_allow(non_literal_match_statement_patterns)]
        match paint.background.tag {
            BackgroundTag::Solid => {
                color = prepared.solid;
            }
            BackgroundTag::LinearGradient => {
                color = linear_gradient_color(paint, position, prepared);
            }
            BackgroundTag::PatternSlash => {
                color = slash_pattern_color(paint, position, prepared.solid);
            }
            BackgroundTag::Checkerboard => {
                color = checkerboard_color(paint, position, prepared.solid);
            }
            BackgroundTag::GradientTable => {
                color = gradient_table_color(paint, position);
            }
        }
        color
    }
    pub fn corner_dash_velocity(first: f32, second: f32) -> f32 {
        if first == 0.0 {
            second
        } else if second == 0.0 {
            first
        } else {
            min(first, second)
        }
    }
    pub fn signed_modulo(value: f32, modulus: f32) -> f32 {
        value - modulus * trunc(value / modulus)
    }
    pub fn dash_coverage(position: f32, period: f32, dash_length: f32, dash_velocity: f32) -> f32 {
        let half_period = period / 2.0;
        let half_dash_length = dash_length / 2.0;
        let centered =
            signed_modulo(position + half_period - half_dash_length, period) - half_period;
        let signed_distance = abs(centered) - half_dash_length;
        antialiased_coverage(signed_distance / dash_velocity)
    }
    pub fn quarter_ellipse_signed_distance(point: Vec2f, radii: Vec2f) -> f32 {
        (length(point / radii) - 1.0) * (radii.x + radii.y) * -0.5
    }
}

pub use source::*;
