#[wgsl_rs::wgsl]
pub mod surface {
    use super::super::common::*;
    use wgsl_rs::std::*;

    #[repr(C)]
    #[derive(Clone, Copy, Wgsl)]
    pub struct SurfaceUniforms {
        pub bounds: Bounds,
        pub content_mask: ContentMask,
        pub color_format: SurfaceColorFormat,
        pub opacity: f32,
        pub padding0: u32,
        pub padding1: u32,
        pub padding2: u32,
        pub padding3: u32,
        pub padding4: u32,
        pub padding5: u32,
    }
    uniform!(group(1), binding(0), SURFACE_LOCALS: SurfaceUniforms);
    texture!(group(1), binding(1), SURFACE_TEXTURE: Texture2D<f32>);
    texture!(group(1), binding(2), SURFACE_CHROMA_TEXTURE: Texture2D<f32>);
    sampler!(group(1), binding(3), SURFACE_SAMPLER: Sampler);

    pub const YCBCR_TO_LINEAR_RGB: Mat4x4f = mat4x4f(
        vec4f(1.0000, 1.0000, 1.0000, 0.0),
        vec4f(0.0000, -0.3441, 1.7720, 0.0),
        vec4f(1.4020, -0.7141, 0.0000, 0.0),
        vec4f(-0.7010, 0.5291, -0.8860, 1.0),
    );

    pub fn sample_yuv_surface(texture_position: Vec2f) -> Vec4f {
        let luma = texture_sample_level(SURFACE_TEXTURE, SURFACE_SAMPLER, texture_position, 0.0).x;
        let chroma = texture_sample_level(
            SURFACE_CHROMA_TEXTURE,
            SURFACE_SAMPLER,
            texture_position,
            0.0,
        )
        .xy();
        YCBCR_TO_LINEAR_RGB * vec4f(luma, chroma.x, chroma.y, 1.0)
    }

    #[derive(Wgsl)]
    pub struct SurfaceVarying {
        #[builtin(position)]
        pub position: Vec4f,
        #[location(0)]
        pub texture_position: Vec2f,
        #[location(3)]
        pub clip_distances: Vec4f,
    }

    #[vertex]
    pub fn vertex_surface(#[builtin(vertex_index)] vertex_id: u32) -> SurfaceVarying {
        let vertex = rectangle_vertex(vertex_id, get!(SURFACE_LOCALS).bounds);
        SurfaceVarying {
            position: vertex.clip_position,
            texture_position: vertex.unit_position,
            clip_distances: clip_distances(
                vertex.viewport_position,
                get!(SURFACE_LOCALS).content_mask.bounds,
            ),
        }
    }

    #[fragment]
    pub fn fragment_surface(input: SurfaceVarying) -> Vec4f {
        if is_clipped(input.clip_distances) {
            return transparent();
        }
        let locals = get!(SURFACE_LOCALS);
        let fade = ContentMask::alpha(locals.content_mask, input.position.xy());
        if locals.color_format == SurfaceColorFormat::Yuv {
            return sample_yuv_surface(input.texture_position) * locals.opacity * fade;
        }
        texture_sample_level(
            SURFACE_TEXTURE,
            SURFACE_SAMPLER,
            input.texture_position,
            0.0,
        ) * locals.opacity
            * fade
    }
}

#[wgsl_rs::wgsl]
pub mod blur {
    use super::super::common::*;
    use super::super::corner_smoothing::*;
    use wgsl_rs::std::*;

    #[repr(C)]
    #[derive(Clone, Copy, Wgsl)]
    pub struct BlurUniforms {
        pub bounds: Bounds,
        pub content_mask: Bounds,
        pub corner_radii: Corners,
        pub direction: Vec2f,
        pub standard_deviation: f32,
        pub opacity: f32,
        pub sample_count: u32,
        pub sample_step: f32,
        pub composite_clip: BlurCompositeClip,
        pub downsample_mode: DownsampleMode,
        pub source_size: Vec2f,
        pub target_size: Vec2f,
        pub corner_smoothing: f32,
        pub padding0: u32,
        pub padding1: u32,
        pub padding2: u32,
    }
    uniform!(group(1), binding(0), BLUR_LOCALS: BlurUniforms);
    texture!(group(1), binding(1), BLUR_TEXTURE: Texture2D<f32>);
    sampler!(group(1), binding(2), BLUR_SAMPLER: Sampler);

    // rev-15 addition (browser-core ticket 39): the composed colour
    // matrix of the filter chain's colour-matrix filters, delivered in
    // its own constant buffer so the pinned `BlurUniforms` layout is
    // untouched. The rows are the first four columns of the 4x5 CSS
    // matrix; the offset vector is the fifth column: out =
    // row_i · (r, g, b, a) + offset_i. `enabled` is 0 for blur-only
    // chains (the composite then reproduces the pinned pass exactly).
    #[repr(C)]
    #[derive(Clone, Copy, Wgsl)]
    pub struct ColorMatrixUniforms {
        pub row0: Vec4f,
        pub row1: Vec4f,
        pub row2: Vec4f,
        pub row3: Vec4f,
        pub offset: Vec4f,
        pub sample_offset: Vec2f,
        pub enabled: u32,
        pub padding0: u32,
        pub padding1: u32,
        pub padding2: u32,
        pub padding3: u32,
        pub padding4: u32,
    }
    uniform!(group(1), binding(3), COLOR_MATRIX_LOCALS: ColorMatrixUniforms);

    // rev-15 addition (browser-core ticket 39): the backdrop mirror
    // crop. CSS crops the backdrop-filter input to the element's border
    // box (filter-effects-2: the backdrop image is the backdrop
    // "clipped to the border box"; Chromium additionally mirrors the
    // edge pixels, viz skia_renderer.cc:3254 @ 0d4dcd4f using
    // SkTileMode::kMirror). The pinned path blurs the whole current
    // target, so content just outside the element bleeds in. The crop
    // entry points copy the element's region of the backdrop into a
    // dedicated texture (the blur passes then read it with mirror
    // addressing, so the kernel's reads beyond the edge fold back onto
    // the element's own content), and the composite samples it with the
    // region-relative coordinate `sample_offset` carries.
    #[repr(C)]
    #[derive(Clone, Copy, Wgsl)]
    pub struct BackdropCropUniforms {
        /// The region's origin in the backdrop texture (device pixels).
        pub origin: Vec2f,
        /// The backdrop texture's size (device pixels).
        pub source_size: Vec2f,
        /// The cropped region's size (device pixels, the element's
        /// pixel-covered bounds).
        pub region_size: Vec2f,
        /// The mirrored margin around the region (device pixels); the
        /// crop texture is `region + 2*margin` so the blur kernel's
        /// reads past the region fold back onto the element's content.
        pub margin: Vec2f,
    }
    uniform!(group(1), binding(4), BACKDROP_CROP_LOCALS: BackdropCropUniforms);

    #[vertex]
    pub fn vertex_backdrop_crop(
        #[builtin(vertex_index)] vertex_id: u32,
    ) -> BlurVarying {
        let vertex = fullscreen_vertex(vertex_id);
        BlurVarying {
            position: vertex.clip_position,
            texture_coordinates: vertex.texture_coordinates,
            clip_distances: unclipped_distances(),
        }
    }

    /// The SkTileMode::kMirror fold: coordinates beyond `[0, extent)`
    /// reflect back into it (Chromium's backdrop edge handling,
    /// components/viz/service/display/skia_renderer.cc:3254).
    pub fn mirror_fold(value: f32, extent: f32) -> f32 {
        let folded = abs(signed_modulo(value, 2.0 * extent));
        select(folded, 2.0 * extent - folded, folded > extent)
    }

    #[fragment]
    pub fn fragment_backdrop_crop(input: BlurVarying) -> Vec4f {
        // The crop texture spans `[−margin, region + margin)` around the
        // region; a destination pixel's local coordinate is folded back
        // into `[0, region)` with mirroring, then offset by the region's
        // origin in the backdrop texture.
        let locals = get!(BACKDROP_CROP_LOCALS);
        let local = input.position.xy() - locals.margin;
        let folded = vec2f(
            mirror_fold(local.x, locals.region_size.x),
            mirror_fold(local.y, locals.region_size.y),
        );
        let coordinates = (folded + locals.origin) / locals.source_size;
        texture_sample_level(BLUR_TEXTURE, BLUR_SAMPLER, coordinates, 0.0)
    }

    #[derive(Clone, Copy, Wgsl)]
    pub struct BlurCompositeVertexData {
        pub position: Vec4f,
        pub texture_coordinates: Vec2f,
        pub clip_distances: Vec4f,
    }

    pub fn prepare_blur_composite_vertex(vertex_id: u32) -> BlurCompositeVertexData {
        let vertex = rectangle_vertex(vertex_id, get!(BLUR_LOCALS).bounds);
        BlurCompositeVertexData {
            position: vertex.clip_position,
            texture_coordinates: vertex.unit_position,
            clip_distances: clip_distances(
                vertex.viewport_position,
                get!(BLUR_LOCALS).content_mask,
            ),
        }
    }

    pub fn blur_composite_color(position: Vec2f, coverage: f32) -> Vec4f {
        // rev-15: the backdrop path samples the crop texture, so the
        // coordinate is region-relative (the content path's offset is
        // (0, 0) and reproduces the pinned formula exactly).
        let uv = (position - get!(COLOR_MATRIX_LOCALS).sample_offset)
            / get!(BLUR_LOCALS).target_size;
        let mut blurred = texture_sample_level(BLUR_TEXTURE, BLUR_SAMPLER, uv, 0.0);
        // rev-15 addition (browser-core ticket 39): apply the composed
        // colour-matrix filters. The blurred texture holds premultiplied
        // content (every scene fragment emits premultiplied colors), so
        // the color is un-premultiplied, mapped through the 4x5 matrix
        // (filter-effects-1 §FilterProperty operates on non-premultiplied
        // RGBA; CSS's colour functions are all 4x5 matrices), clamped to
        // the filter region's [0, 1], and re-premultiplied.
        let matrix = get!(COLOR_MATRIX_LOCALS);
        if matrix.enabled != 0u32 {
            let alpha = blurred.w;
            let straight = select(
                transparent(),
                blurred / vec4f(alpha, alpha, alpha, 1.0),
                alpha > 0.0,
            );
            let mapped = vec4f(
                dot(matrix.row0, straight) + matrix.offset.x,
                dot(matrix.row1, straight) + matrix.offset.y,
                dot(matrix.row2, straight) + matrix.offset.z,
                dot(matrix.row3, straight) + matrix.offset.w,
            );
            let clamped = saturate(mapped);
            blurred = clamped * vec4f(
                clamped.w,
                clamped.w,
                clamped.w,
                1.0,
            );
        }
        let factor = coverage * get!(BLUR_LOCALS).opacity;
        vec4f(
            blurred.x * factor,
            blurred.y * factor,
            blurred.z * factor,
            blurred.w * factor,
        )
    }

    #[derive(Wgsl)]
    pub struct BlurVarying {
        #[builtin(position)]
        pub position: Vec4f,
        #[location(0)]
        pub texture_coordinates: Vec2f,
        #[location(3)]
        pub clip_distances: Vec4f,
    }

    #[vertex]
    pub fn vertex_blur_fullscreen(#[builtin(vertex_index)] vertex_id: u32) -> BlurVarying {
        let vertex = fullscreen_vertex(vertex_id);
        BlurVarying {
            position: vertex.clip_position,
            texture_coordinates: vertex.texture_coordinates,
            clip_distances: unclipped_distances(),
        }
    }

    #[fragment]
    pub fn fragment_blur_downsample(input: BlurVarying) -> Vec4f {
        if get!(BLUR_LOCALS).downsample_mode == DownsampleMode::HalfResolution {
            let destination = floor(input.position.xy());
            let source_coordinates = min(
                (destination + 0.5) / get!(BLUR_LOCALS).target_size,
                (get!(BLUR_LOCALS).source_size - 0.5) / get!(BLUR_LOCALS).source_size,
            );
            return texture_sample_level(BLUR_TEXTURE, BLUR_SAMPLER, source_coordinates, 0.0);
        }
        texture_sample_level(BLUR_TEXTURE, BLUR_SAMPLER, input.texture_coordinates, 0.0)
    }

    pub fn sample_blur_texture(texture_coordinates: Vec2f) -> Vec4f {
        texture_sample_level(BLUR_TEXTURE, BLUR_SAMPLER, texture_coordinates, 0.0)
    }

    pub fn gaussian_blur(texture_coordinates: Vec2f) -> Vec4f {
        let uniforms = get!(BLUR_LOCALS);
        let center_weight = gaussian(0.0, uniforms.standard_deviation);
        let mut weighted_color = sample_blur_texture(texture_coordinates) * center_weight;
        let mut weight_sum = center_weight;
        let mut sample_index = 1u32;

        while sample_index <= uniforms.sample_count {
            let distance = sample_index as f32 * uniforms.sample_step;
            let weight = gaussian(distance, uniforms.standard_deviation);
            let coordinate_offset = uniforms.direction * distance;
            let symmetric_pair = sample_blur_texture(texture_coordinates - coordinate_offset)
                + sample_blur_texture(texture_coordinates + coordinate_offset);
            weighted_color = weighted_color + symmetric_pair * weight;
            weight_sum += 2.0 * weight;
            sample_index += 1u32;
        }
        weighted_color / max(weight_sum, MIN_NORMALIZED_WEIGHT)
    }

    #[fragment]
    pub fn fragment_blur(input: BlurVarying) -> Vec4f {
        gaussian_blur(input.texture_coordinates)
    }

    #[vertex]
    pub fn vertex_blur_composite(#[builtin(vertex_index)] vertex_id: u32) -> BlurVarying {
        let vertex = prepare_blur_composite_vertex(vertex_id);
        BlurVarying {
            position: vertex.position,
            texture_coordinates: vertex.texture_coordinates,
            clip_distances: vertex.clip_distances,
        }
    }

    #[fragment]
    pub fn fragment_blur_composite(input: BlurVarying) -> Vec4f {
        if is_clipped(input.clip_distances) {
            return transparent();
        }
        let coverage = select(
            1.0,
            antialiased_coverage(rounded_rectangle_signed_distance(
                input.position.xy(),
                get!(BLUR_LOCALS).bounds,
                get!(BLUR_LOCALS).corner_radii,
            )),
            get!(BLUR_LOCALS).composite_clip == BlurCompositeClip::RoundedBounds,
        );
        blur_composite_color(input.position.xy(), coverage)
    }

    #[derive(Wgsl)]
    pub struct SmoothedBlurVarying {
        #[builtin(position)]
        pub position: Vec4f,
        #[location(0)]
        pub texture_coordinates: Vec2f,
        #[location(1)]
        #[interpolate(flat)]
        pub horizontal_corner_reaches: Vec4f,
        #[location(2)]
        #[interpolate(flat)]
        pub vertical_corner_reaches: Vec4f,
        #[location(3)]
        pub clip_distances: Vec4f,
        #[location(4)]
        #[interpolate(flat)]
        pub smoothing_factors: Vec4f,
        #[location(5)]
        #[interpolate(flat)]
        pub superellipse_power: f32,
    }

    #[vertex]
    pub fn vertex_smoothed_blur_composite(
        #[builtin(vertex_index)] vertex_id: u32,
    ) -> SmoothedBlurVarying {
        let vertex = prepare_blur_composite_vertex(vertex_id);
        let prepared = prepare_corners(
            get!(BLUR_LOCALS).bounds.size,
            get!(BLUR_LOCALS).corner_radii,
            get!(BLUR_LOCALS).corner_smoothing,
            true,
        );
        SmoothedBlurVarying {
            position: vertex.position,
            texture_coordinates: vertex.texture_coordinates,
            horizontal_corner_reaches: prepared.horizontal_reaches,
            vertical_corner_reaches: prepared.vertical_reaches,
            clip_distances: vertex.clip_distances,
            smoothing_factors: prepared.smoothing_factors,
            superellipse_power: prepared.superellipse_power,
        }
    }

    #[fragment]
    pub fn fragment_smoothed_blur_composite(input: SmoothedBlurVarying) -> Vec4f {
        if is_clipped(input.clip_distances) {
            return transparent();
        }
        let coverage = select(
            1.0,
            antialiased_coverage(prepared_corner_signed_distance(
                input.position.xy(),
                get!(BLUR_LOCALS).bounds,
                get!(BLUR_LOCALS).corner_radii,
                get!(BLUR_LOCALS).corner_smoothing,
                PreparedCorners {
                    horizontal_reaches: input.horizontal_corner_reaches,
                    vertical_reaches: input.vertical_corner_reaches,
                    smoothing_factors: input.smoothing_factors,
                    superellipse_power: input.superellipse_power,
                },
            )),
            get!(BLUR_LOCALS).composite_clip == BlurCompositeClip::RoundedBounds,
        );
        blur_composite_color(input.position.xy(), coverage)
    }
}
